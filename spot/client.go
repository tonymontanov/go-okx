/*
ФАЙЛ: spot/client.go

ОПИСАНИЕ:
Корневой клиент SPOT-профиля. Зеркально swap/client.go: parent okx.Client,
четыре саб-клиента, общие public/private WS-соединения, регистрация фабрики
в init().

ОТЛИЧИЯ ОТ SWAP:
  - Использует SPOT-специфичные эндпоинты и типы (spot/types).
  - На WS используется тот же хост (public и private OKX v5 одни и те же
    для всех instType).
  - tdMode по умолчанию `cash`.

ОСОБЕННОСТИ ID:
  - clOrdId/tag — те же ограничения OKX [A-Za-z0-9]{1,32} / {1,16}.
    Валидация — в spot/trading.go.

ПОТОКОБЕЗОПАСНОСТЬ:
  - Client потокобезопасен: саб-клиенты — read-only после конструирования.
  - WS-соединения создаются лениво через sync.Once.

УПОТРЕБЛЕНИЕ:
  - Через корневой okx.Client:
        var spotClient *spot.Client = okxClient.Spot().(*spot.Client)
  - Напрямую (для тестов / тонкого контроля):
        var spotClient *spot.Client = spot.NewClient(okxClient)
*/

package spot

import (
	"sync"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/ws"
)

// Client — клиент SPOT-профиля OKX.
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

// NewClient создаёт SPOT-клиент. Параметр parent обязателен.
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

// Trading возвращает саб-клиент торговли SPOT.
func (c *Client) Trading() *TradingClient { return c.trading }

// Account возвращает саб-клиент баланса SPOT.
func (c *Client) Account() *AccountClient { return c.account }

// MarketData возвращает саб-клиент рыночных данных SPOT.
func (c *Client) MarketData() *MarketDataClient { return c.marketData }

// Stream возвращает саб-клиент WS-подписок SPOT.
func (c *Client) Stream() *StreamClient { return c.stream }

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

// init регистрирует фабрику в корневом пакете. Зеркально swap.init().
//
// Достаточно blank-import:
//
//	import _ "github.com/tonymontanov/go-okx/v2/spot"
//
// чтобы okx.Client.Spot() начал возвращать *spot.Client.
func init() {
	okx.RegisterSpotFactory(func(parent *okx.Client) any {
		return NewClient(parent)
	})
}
