/*
FILE: spot/client.go

DESCRIPTION:
Root client for the SPOT profile. Mirrors swap/client.go: parent okx.Client,
four sub-clients, shared public/private WS connections, factory registration in init().

DIFFERENCES FROM SWAP:
  - Uses SPOT-specific endpoints and types (spot/types).
  - The same WS host is used (OKX v5 public and private hosts are shared
    across all instTypes).
  - tdMode defaults to `cash`.

ID CONSTRAINTS:
  - clOrdId/tag — same OKX restrictions [A-Za-z0-9]{1,32} / {1,16}.
    Validation is in spot/trading.go.

THREAD SAFETY:
  - Client is thread-safe: sub-clients are read-only after construction.
  - WS connections are created lazily via sync.Once.

USAGE:
  - Via the root okx.Client:
        var spotClient *spot.Client = okxClient.Spot().(*spot.Client)
  - Directly (for tests / fine-grained control):
        var spotClient *spot.Client = spot.NewClient(okxClient)
*/

package spot

import (
	"sync"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/ws"
)

// Client — OKX SPOT profile client.
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

// NewClient creates a SPOT client. The parent parameter is required.
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

// Parent returns the root okx.Client.
func (c *Client) Parent() *okx.Client { return c.parent }

// Trading returns the SPOT trading sub-client.
func (c *Client) Trading() *TradingClient { return c.trading }

// Account returns the SPOT account/balance sub-client.
func (c *Client) Account() *AccountClient { return c.account }

// MarketData returns the SPOT market data sub-client.
func (c *Client) MarketData() *MarketDataClient { return c.marketData }

// Stream returns the SPOT WS subscriptions sub-client.
func (c *Client) Stream() *StreamClient { return c.stream }

func (c *Client) logger() okx.Logger  { return c.parent.Logger() }
func (c *Client) rest() restDoer      { return c.parent.REST() }
func (c *Client) config() okx.Config  { return c.parent.Config() }
func (c *Client) signerEnabled() bool { return c.parent.Signer().Enabled() }

// publicConn returns the public WS connection, creating it lazily.
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

// privateConn returns the private WS connection, creating it lazily.
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

// toWsConfig converts the public okx.WsConfig to a local ws.Config.
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

// init registers the factory in the root package. Mirrors swap.init().
//
// A blank-import is sufficient:
//
//	import _ "github.com/tonymontanov/go-okx/v2/spot"
//
// for okx.Client.Spot() to start returning *spot.Client.
func init() {
	okx.RegisterSpotFactory(func(parent *okx.Client) any {
		return NewClient(parent)
	})
}
