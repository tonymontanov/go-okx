/*
ФАЙЛ: client.go

ОПИСАНИЕ:
Главный публичный Client SDK. Хранит shared-ресурсы (REST-клиент, signer,
конфиг, логгер) и раздаёт «ленивых» доменных саб-клиентов по требованию.
В v1 поддержан только SWAP-профиль; SPOT-профиль зарезервирован и реализуется
отдельной итерацией.

ОСНОВНЫЕ ФУНКЦИИ:
  - NewClient(cfg)        : конструктор с проверкой Config и установкой defaults.
  - (Client).Swap()       : возвращает саб-клиент SWAP-профиля (instType=SWAP).
                            Создаётся лениво при первом обращении.
  - (Client).Close()      : корректно завершает фоновые операции (WS-стримы,
                            пулы соединений). Не блокирующая.

ОСНОВНЫЕ СУЩНОСТИ:
  - Client          : корневой объект SDK.
  - swapClientCtor  : внутренний контракт, через который пакет swap создаёт
                      свой клиент. Это позволяет избежать import-cycle между
                      корнем (где живёт Client) и swap (где живёт SwapClient).

ЗАВИСИМОСТИ:
- internal/auth, internal/rest: подпись и REST-транспорт.
- sync: ленивая инициализация саб-клиентов.
*/

package okx

import (
	"sync"

	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/rest"
)

// Client — корневой объект SDK.
type Client struct {
	cfg    Config
	signer *auth.Signer
	rest   *rest.Client
	logger Logger

	swapOnce sync.Once
	swapVal  any

	spotOnce sync.Once
	spotVal  any
}

// NewClient создаёт корневой клиент SDK. cfg проходит withDefaults + validate.
// Если credentials заданы — Signer будет enabled и подпишет приватные вызовы;
// иначе клиент сможет работать только с public endpoint'ами.
func NewClient(cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()
	var err error = cfg.validate()
	if err != nil {
		return nil, err
	}

	var signer *auth.Signer = auth.NewSigner(cfg.APIKey, cfg.SecretKey, cfg.Passphrase)
	var restCfg rest.Config = rest.Config{
		RequestTimeout:      cfg.REST.RequestTimeout,
		MaxIdleConns:        cfg.REST.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.REST.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.REST.IdleConnTimeout,
		Demo:                cfg.Demo,
		RateLimitObserver:   cfg.RateLimitObserver,
	}
	// Прокидываем новый event-observer через тонкий адаптер. Структура
	// RateLimitEvent живёт в корневом okx-пакете и НЕ может пробрасываться
	// напрямую в internal/rest (import-cycle). Поэтому rest вызывает
	// callback с плоскими аргументами (endpoint, method, headers, meta),
	// а мы здесь собираем RateLimitEvent для конечного подписчика.
	if cfg.RateLimitEventObserver != nil {
		var userObserver func(RateLimitEvent) = cfg.RateLimitEventObserver
		restCfg.RateLimitEventObserver = func(endpoint, method string, headers map[string]string, meta rest.RequestMeta) {
			userObserver(RateLimitEvent{
				Endpoint:   endpoint,
				Method:     method,
				Headers:    headers,
				OrderCount: meta.OrderCount,
				Symbols:    meta.Symbols,
				Category:   RateLimitCategory(meta.Category),
			})
		}
	}
	var restClient *rest.Client = rest.NewClient(cfg.REST.BaseURL, signer, restCfg, cfg.UserAgent, cfg.Logger)

	return &Client{
		cfg:    cfg,
		signer: signer,
		rest:   restClient,
		logger: cfg.Logger,
	}, nil
}

// Config возвращает копию финального конфига (после withDefaults). Полезно для
// диагностики и метрик.
func (c *Client) Config() Config { return c.cfg }

// Logger возвращает текущий логгер.
func (c *Client) Logger() Logger { return c.logger }

// Signer возвращает internal/auth.Signer (для внутренних подпакетов SDK).
// Экспортируется для использования из swap/spot подпакетов — пользовательский
// код не должен брать signer напрямую.
func (c *Client) Signer() *auth.Signer { return c.signer }

// REST возвращает internal/rest.Client (для внутренних подпакетов SDK).
func (c *Client) REST() *rest.Client { return c.rest }

// Close высвобождает ресурсы (idle HTTP-соединения). Безопасно вызывать
// несколько раз. WS-стримы завершаются по отмене своих контекстов.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.rest.Close()
	return nil
}

// swapClientFactory — функция-строитель swap-клиента. Регистрируется пакетом
// swap через RegisterSwapFactory в init(). Это обходит import-cycle.
var swapClientFactory func(c *Client) any

// RegisterSwapFactory регистрирует фабрику swap-клиента. Должна вызываться из
// init() пакета swap. Идемпотентна.
func RegisterSwapFactory(f func(c *Client) any) {
	if swapClientFactory == nil {
		swapClientFactory = f
	}
}

// Swap возвращает swap-саб-клиент. Тип результата — any, потому что корневой
// пакет не может импортировать swap (тот импортирует корень). Вызывающий код
// сразу type-assert'ит к *swap.Client.
//
// Идиома использования:
//
//	var swapClient *swap.Client = client.Swap().(*swap.Client)
//
// Lazy: создаётся при первом обращении через зарегистрированную фабрику.
func (c *Client) Swap() any {
	c.swapOnce.Do(func() {
		if swapClientFactory == nil {
			c.logger.Warn("okx.Client.Swap: swap factory is not registered; import _ \"github.com/tonymontanov/go-okx/v2/swap\"")
			return
		}
		c.swapVal = swapClientFactory(c)
	})
	return c.swapVal
}

// spotClientFactory — функция-строитель spot-клиента. Зарегистрируется пакетом
// spot в init() ровно как swap (см. RegisterSwapFactory).
var spotClientFactory func(c *Client) any

// RegisterSpotFactory регистрирует фабрику spot-клиента. Должна вызываться из
// init() пакета spot. Идемпотентна.
//
// Spot и Swap — независимые домены: подключение одного НЕ требует второго.
// Это позволяет приложениям импортировать только нужный профиль и не
// тянуть в бинарь лишний код:
//
//	import _ "github.com/tonymontanov/go-okx/v2/spot"  // только spot
//	import _ "github.com/tonymontanov/go-okx/v2/swap"  // только swap
//
// Корневой пакет okx НЕ импортирует ни spot, ни swap — это обходит
// import-cycle (оба пакета импортируют корневой okx для okx.Config /
// okx.NewError и т.д.).
func RegisterSpotFactory(f func(c *Client) any) {
	if spotClientFactory == nil {
		spotClientFactory = f
	}
}

// Spot возвращает spot-саб-клиент. См. Swap() — семантика идентична.
//
// Идиома использования:
//
//	var spotClient *spot.Client = client.Spot().(*spot.Client)
//
// Lazy: создаётся при первом обращении через зарегистрированную фабрику.
// Если пакет spot не импортирован (фабрика не зарегистрирована) —
// возвращается nil и пишется warn в логгер.
func (c *Client) Spot() any {
	c.spotOnce.Do(func() {
		if spotClientFactory == nil {
			c.logger.Warn("okx.Client.Spot: spot factory is not registered; import _ \"github.com/tonymontanov/go-okx/v2/spot\"")
			return
		}
		c.spotVal = spotClientFactory(c)
	})
	return c.spotVal
}
