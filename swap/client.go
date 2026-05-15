/*
ФАЙЛ: swap/client.go

ОПИСАНИЕ:
Корневой клиент SWAP-профиля. Хранит ссылку на parent okx.Client (REST,
Signer, Logger, Config) и предоставляет четыре доменных саб-клиента.

ОСНОВНЫЕ ФУНКЦИИ:
  - NewClient(parent)        : конструктор. Регистрируется в okx через init.
  - (*Client).Trading()      : доменный саб-клиент торговли.
  - (*Client).Account()      : доменный саб-клиент позиций/счетов.
  - (*Client).MarketData()   : доменный саб-клиент рыночных данных.
  - (*Client).Stream()       : доменный саб-клиент WS-подписок (заглушка в M1).

КОНТРАКТ:
  - Client потокобезопасен: саб-клиенты — read-only после конструирования.
  - Все REST-вызовы идут через parent.REST() — общий пул соединений.

ОСОБЕННОСТИ ID:
  - clOrdId в OKX ограничен 32 символами [A-Za-z0-9_], тег `tag` — 16. SDK
    эту проверку делает на этапе сборки запроса (см. trading.go) и
    возвращает ErrorKindInvalidRequest без отправки.
*/

package swap

import (
	"sync"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/ws"
)

// Client — клиент SWAP-профиля OKX.
type Client struct {
	parent *okx.Client

	trading    *TradingClient
	account    *AccountClient
	marketData *MarketDataClient
	stream     *StreamClient

	publicWsOnce  sync.Once
	publicWs      *ws.Conn
	privateWsOnce sync.Once
	privateWs     *ws.Conn
}

// NewClient создаёт SWAP-клиент. Параметр parent обязателен.
func NewClient(parent *okx.Client) *Client {
	if parent == nil {
		return nil
	}
	var c *Client = &Client{parent: parent}
	c.trading = newTradingClient(c)
	c.account = newAccountClient(c)
	c.marketData = newMarketDataClient(c)
	c.stream = newStreamClient(c)
	return c
}

// Parent возвращает корневой okx.Client.
func (c *Client) Parent() *okx.Client { return c.parent }

// Trading возвращает саб-клиент торговли.
func (c *Client) Trading() *TradingClient { return c.trading }

// Account возвращает саб-клиент позиций/счетов.
func (c *Client) Account() *AccountClient { return c.account }

// MarketData возвращает саб-клиент рыночных данных.
func (c *Client) MarketData() *MarketDataClient { return c.marketData }

// Stream возвращает саб-клиент WS-подписок.
func (c *Client) Stream() *StreamClient { return c.stream }

// logger / rest / signer — внутренние шорткаты для саб-клиентов.
func (c *Client) logger() okx.Logger  { return c.parent.Logger() }
func (c *Client) rest() restDoer      { return c.parent.REST() }
func (c *Client) config() okx.Config  { return c.parent.Config() }
func (c *Client) signerEnabled() bool { return c.parent.Signer().Enabled() }

// publicConn возвращает (лениво создавая) public WS-соединение.
func (c *Client) publicConn() *ws.Conn {
	c.publicWsOnce.Do(func() {
		var cfg okx.Config = c.parent.Config()
		c.publicWs = ws.NewConn(
			toWsConfig(cfg, cfg.WS.PublicURL, false),
			c.parent.Signer(),
			cfg.Logger,
			cfg.Metrics,
		)
	})
	return c.publicWs
}

// privateConn возвращает (лениво создавая) private WS-соединение.
func (c *Client) privateConn() *ws.Conn {
	c.privateWsOnce.Do(func() {
		var cfg okx.Config = c.parent.Config()
		c.privateWs = ws.NewConn(
			toWsConfig(cfg, cfg.WS.PrivateURL, true),
			c.parent.Signer(),
			cfg.Logger,
			cfg.Metrics,
		)
	})
	return c.privateWs
}

// toWsConfig конвертирует публичный okx.WsConfig в локальный ws.Config.
func toWsConfig(cfg okx.Config, url string, private bool) ws.Config {
	return ws.Config{
		URL:                     url,
		IsPrivate:               private,
		HandshakeTimeout:        cfg.WS.HandshakeTimeout,
		ReadTimeout:             cfg.WS.ReadTimeout,
		WriteTimeout:            cfg.WS.WriteTimeout,
		PingInterval:            cfg.WS.PingInterval,
		ReconnectInitialBackoff: cfg.WS.ReconnectInitialBackoff,
		ReconnectMaxBackoff:     cfg.WS.ReconnectMaxBackoff,
		ReconnectJitter:         cfg.WS.ReconnectJitter,
		ReadBufferSize:          cfg.WS.ReadBufferSize,
		WriteBufferSize:         cfg.WS.WriteBufferSize,
	}
}

// init регистрирует фабрику в корневом пакете, чтобы okx.Client.Swap() лениво
// возвращал *swap.Client. Это позволяет пользователю не тащить swap-импорт
// руками, если он работает только через корневой Client (но при таком стиле
// нужен blank-import "github.com/tonymontanov/go-okx/v2/swap").
func init() {
	okx.RegisterSwapFactory(func(parent *okx.Client) any {
		return NewClient(parent)
	})
}
