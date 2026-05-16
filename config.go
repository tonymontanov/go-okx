/*
ФАЙЛ: config.go

ОПИСАНИЕ:
Файл config.go определяет конфигурационные структуры SDK (см. ТЗ §5.5).
Здесь же — типичные production / demo endpoints OKX и значения по умолчанию.

ОСНОВНЫЕ ФУНКЦИИ:
  - DefaultConfig(): возвращает Config c production-endpoints и значениями
    по умолчанию для таймаутов/reconnect/orderbook.
  - (Config).withDefaults(): добивает пустые поля Config дефолтами. Внутри
    SDK конфиг ВСЕГДА сначала пропускается через withDefaults().

ОСНОВНЫЕ СУЩНОСТИ:
  - Config:                    публичная конфигурация Client'а.
  - RestConfig / WsConfig:     транспортные параметры (таймауты, reconnect, ping).
  - OrderbookConfig:           параметры orderbook engine (depth, resync policy).

ЭНДПОИНТЫ:
По дефолту используются production-хосты OKX:
  - REST: https://www.okx.com
  - WS public:  wss://ws.okx.com:8443/ws/v5/public
  - WS private: wss://ws.okx.com:8443/ws/v5/private
  - WS business: wss://ws.okx.com:8443/ws/v5/business

Демо-режим (`x-simulated-trading: 1`) — вне Scope v1 (см. ответы на ТЗ).

ЗАВИСИМОСТИ:
Стандартные библиотеки:
  - time: таймауты и интервалы reconnect/keepalive.
*/

package okx

import "time"

// Транспортные URL'ы OKX. Объявлены как vars, а не const, чтобы тесты могли
// переопределить (например, на mock-сервер).
var (
	// DefaultRestBaseURL — production REST endpoint OKX v5. Используется и в
	// production, и в demo (для demo нужен лишь заголовок x-simulated-trading: 1).
	DefaultRestBaseURL string = "https://www.okx.com"
	// DefaultWsPublicURL — production WS endpoint для публичных каналов
	// (books/tickers/marks/index).
	DefaultWsPublicURL string = "wss://ws.okx.com:8443/ws/v5/public"
	// DefaultWsPrivateURL — production WS endpoint для приватных каналов
	// (orders/positions/account).
	DefaultWsPrivateURL string = "wss://ws.okx.com:8443/ws/v5/private"
	// DefaultWsBusinessURL — production WS endpoint для business-каналов
	// (algo-orders, deposit-info, candles). Зарезервирован на будущее.
	DefaultWsBusinessURL string = "wss://ws.okx.com:8443/ws/v5/business"

	// DemoWsPublicURL — demo (paper-trading) WS endpoint для публичных каналов.
	// OKX выделяет под demo отдельный хост wspap.okx.com.
	DemoWsPublicURL string = "wss://wspap.okx.com:8443/ws/v5/public"
	// DemoWsPrivateURL — demo WS endpoint для приватных каналов.
	DemoWsPrivateURL string = "wss://wspap.okx.com:8443/ws/v5/private"
	// DemoWsBusinessURL — demo WS endpoint для business-каналов.
	DemoWsBusinessURL string = "wss://wspap.okx.com:8443/ws/v5/business"
)

// Config — публичная конфигурация SDK. Передаётся в NewClient.
type Config struct {
	// APIKey — публичный ключ OKX (OK-ACCESS-KEY).
	APIKey string
	// SecretKey — секретный ключ OKX, используемый для HMAC-подписи (OK-ACCESS-SIGN).
	SecretKey string
	// Passphrase — обязательный для OKX passphrase (OK-ACCESS-PASSPHRASE).
	Passphrase string

	// REST — настройки REST-транспорта. Если пуст — берётся DefaultConfig().REST.
	REST RestConfig
	// WS — настройки WebSocket-транспорта. Если пуст — берётся DefaultConfig().WS.
	WS WsConfig
	// Orderbook — настройки orderbook-движка. Если пуст — DefaultConfig().Orderbook.
	Orderbook OrderbookConfig

	// Logger — опциональный логгер. Если nil — используется NoopLogger().
	Logger Logger

	// Metrics — опциональная фабрика счётчиков. Если nil — используется
	// NoopMetrics(). SDK создаёт через неё следующие счётчики (см. docs):
	//   okx_ws_messages_received_total
	//   okx_ws_messages_dropped_total
	//   okx_ws_reconnects_total
	//   okx_ws_subscriptions_total
	//   okx_ws_ping_failed_total
	Metrics CounterFactory

	// UserAgent — значение User-Agent для REST-запросов. Если пусто — "go-okx/v2".
	UserAgent string

	// Demo — переводит клиент в режим OKX Demo Trading (paper-trading).
	// Эффект:
	//   - REST: ко всем запросам добавляется заголовок "x-simulated-trading: 1".
	//     URL остаётся production (DefaultRestBaseURL), потому что demo и prod
	//     отвечают по одному и тому же хосту — отличает их только заголовок.
	//   - WS:   URL'ы public/private/business АВТОМАТИЧЕСКИ заменяются на
	//     wspap.okx.com, если пользователь не задал их явно через WS.PublicURL
	//     и т. п. Если WS.PublicURL уже задан (например, на mock-сервер) —
	//     SDK его не трогает.
	//   - Ключи нужны отдельные: на OKX demo и prod ключи НЕ совместимы;
	//     создайте demo-ключи в Profile → Demo Trading → API.
	Demo bool
}

// RestConfig — настройки HTTP-транспорта.
type RestConfig struct {
	// BaseURL — базовый URL REST API OKX. По умолчанию DefaultRestBaseURL.
	BaseURL string
	// RequestTimeout — таймаут одного REST-запроса. По умолчанию 10s.
	// Для критичных «hot» вызовов (place/cancel) можно передавать ctx с
	// собственным дедлайном — он перекрывает RequestTimeout.
	RequestTimeout time.Duration
	// MaxIdleConns — размер пула idle-соединений http.Transport. По умолчанию 100.
	MaxIdleConns int
	// MaxIdleConnsPerHost — pool size per host. По умолчанию 100.
	MaxIdleConnsPerHost int
	// IdleConnTimeout — keep-alive idle timeout. По умолчанию 90s.
	IdleConnTimeout time.Duration
}

// WsConfig — настройки WebSocket-транспорта.
type WsConfig struct {
	// PublicURL — URL public WS (orderbook/tickers/...). Default DefaultWsPublicURL.
	PublicURL string
	// PrivateURL — URL private WS (orders/positions). Default DefaultWsPrivateURL.
	PrivateURL string
	// BusinessURL — URL business WS. Default DefaultWsBusinessURL.
	BusinessURL string
	// HandshakeTimeout — таймаут установки соединения. По умолчанию 10s.
	HandshakeTimeout time.Duration
	// ReadTimeout — таймаут чтения одного фрейма. По умолчанию 35s (OKX шлёт ping раз в 30s).
	ReadTimeout time.Duration
	// WriteTimeout — таймаут записи одного фрейма. По умолчанию 5s.
	WriteTimeout time.Duration
	// PingInterval — интервал клиентского ping (OKX: "ping" текстовый фрейм,
	// сервер отвечает "pong"). По умолчанию 20s (требование OKX <30s).
	PingInterval time.Duration
	// ReconnectInitialBackoff — стартовая задержка между попытками реконнекта. По умолчанию 200ms.
	ReconnectInitialBackoff time.Duration
	// ReconnectMaxBackoff — верхняя граница backoff'а. По умолчанию 10s.
	ReconnectMaxBackoff time.Duration
	// ReconnectJitter — относительный jitter [0..1] добавляемый к backoff'у. По умолчанию 0.2.
	ReconnectJitter float64
	// ReadBufferSize — размер read-буфера gorilla/websocket. По умолчанию 64KB.
	ReadBufferSize int
	// WriteBufferSize — размер write-буфера gorilla/websocket. По умолчанию 16KB.
	WriteBufferSize int
}

// OrderbookConfig — настройки orderbook-движка.
type OrderbookConfig struct {
	// MaxDepth — глубина локального стакана (число уровней с каждой стороны).
	// По умолчанию 400 (соответствует канала "books" OKX).
	MaxDepth int
	// ChecksumLevels — число уровней, по которым OKX считает CRC32. Всегда 25
	// согласно спецификации. Параметризовано на случай изменения протокола.
	ChecksumLevels int
	// ChecksumMismatchToResync — сколько подряд несоответствий CRC32 допустимо
	// прежде чем форсировать resync. По умолчанию 1 (любой mismatch — resync).
	ChecksumMismatchToResync int
}

// DefaultConfig возвращает конфигурацию со всеми разумными значениями по
// умолчанию (production-endpoints + production-таймауты).
func DefaultConfig() Config {
	return Config{
		REST: RestConfig{
			BaseURL:             DefaultRestBaseURL,
			RequestTimeout:      10 * time.Second,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
		WS: WsConfig{
			PublicURL:               DefaultWsPublicURL,
			PrivateURL:              DefaultWsPrivateURL,
			BusinessURL:             DefaultWsBusinessURL,
			HandshakeTimeout:        10 * time.Second,
			ReadTimeout:             35 * time.Second,
			WriteTimeout:            5 * time.Second,
			PingInterval:            20 * time.Second,
			ReconnectInitialBackoff: 200 * time.Millisecond,
			ReconnectMaxBackoff:     10 * time.Second,
			ReconnectJitter:         0.2,
			ReadBufferSize:          64 * 1024,
			WriteBufferSize:         16 * 1024,
		},
		Orderbook: OrderbookConfig{
			MaxDepth:                 400,
			ChecksumLevels:           25,
			ChecksumMismatchToResync: 1,
		},
		Logger:    NoopLogger(),
		Metrics:   NoopMetrics(),
		UserAgent: "go-okx/v2",
	}
}

// withDefaults возвращает Config, где все пустые поля заполнены значениями
// из DefaultConfig(). Используется внутри NewClient — пользовательский Config
// никогда не мутируется.
func (c Config) withDefaults() Config {
	var def Config = DefaultConfig()

	if c.REST.BaseURL == "" {
		c.REST.BaseURL = def.REST.BaseURL
	}
	if c.REST.RequestTimeout == 0 {
		c.REST.RequestTimeout = def.REST.RequestTimeout
	}
	if c.REST.MaxIdleConns == 0 {
		c.REST.MaxIdleConns = def.REST.MaxIdleConns
	}
	if c.REST.MaxIdleConnsPerHost == 0 {
		c.REST.MaxIdleConnsPerHost = def.REST.MaxIdleConnsPerHost
	}
	if c.REST.IdleConnTimeout == 0 {
		c.REST.IdleConnTimeout = def.REST.IdleConnTimeout
	}

	// WS endpoints: для Demo выбираем wspap.okx.com, для prod — ws.okx.com.
	// Если пользователь явно задал URL — НИЧЕГО не подменяем (он умнее SDK).
	var defPublic string = def.WS.PublicURL
	var defPrivate string = def.WS.PrivateURL
	var defBusiness string = def.WS.BusinessURL
	if c.Demo {
		defPublic = DemoWsPublicURL
		defPrivate = DemoWsPrivateURL
		defBusiness = DemoWsBusinessURL
	}
	if c.WS.PublicURL == "" {
		c.WS.PublicURL = defPublic
	}
	if c.WS.PrivateURL == "" {
		c.WS.PrivateURL = defPrivate
	}
	if c.WS.BusinessURL == "" {
		c.WS.BusinessURL = defBusiness
	}
	if c.WS.HandshakeTimeout == 0 {
		c.WS.HandshakeTimeout = def.WS.HandshakeTimeout
	}
	if c.WS.ReadTimeout == 0 {
		c.WS.ReadTimeout = def.WS.ReadTimeout
	}
	if c.WS.WriteTimeout == 0 {
		c.WS.WriteTimeout = def.WS.WriteTimeout
	}
	if c.WS.PingInterval == 0 {
		c.WS.PingInterval = def.WS.PingInterval
	}
	if c.WS.ReconnectInitialBackoff == 0 {
		c.WS.ReconnectInitialBackoff = def.WS.ReconnectInitialBackoff
	}
	if c.WS.ReconnectMaxBackoff == 0 {
		c.WS.ReconnectMaxBackoff = def.WS.ReconnectMaxBackoff
	}
	if c.WS.ReconnectJitter == 0 {
		c.WS.ReconnectJitter = def.WS.ReconnectJitter
	}
	if c.WS.ReadBufferSize == 0 {
		c.WS.ReadBufferSize = def.WS.ReadBufferSize
	}
	if c.WS.WriteBufferSize == 0 {
		c.WS.WriteBufferSize = def.WS.WriteBufferSize
	}

	if c.Orderbook.MaxDepth == 0 {
		c.Orderbook.MaxDepth = def.Orderbook.MaxDepth
	}
	if c.Orderbook.ChecksumLevels == 0 {
		c.Orderbook.ChecksumLevels = def.Orderbook.ChecksumLevels
	}
	if c.Orderbook.ChecksumMismatchToResync == 0 {
		c.Orderbook.ChecksumMismatchToResync = def.Orderbook.ChecksumMismatchToResync
	}

	if c.Logger == nil {
		c.Logger = NoopLogger()
	}
	if c.Metrics == nil {
		c.Metrics = NoopMetrics()
	}
	if c.UserAgent == "" {
		c.UserAgent = def.UserAgent
	}

	return c
}

// validate проверяет, что credentials заданы (для подписанных вызовов их
// отсутствие приведёт к ошибке OKX). Публичные REST/WS работают и без ключей —
// поэтому валидация делается «мягко»: пустые поля не запрещаем, но запоминаем
// флаг `signed`, который проверим в auth.Signer.
func (c Config) validate() error {
	if c.REST.BaseURL == "" {
		return NewError(ErrorKindInvalidRequest, "", "config: REST.BaseURL is empty", nil)
	}
	if c.WS.PublicURL == "" {
		return NewError(ErrorKindInvalidRequest, "", "config: WS.PublicURL is empty", nil)
	}
	return nil
}
