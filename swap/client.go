/*
FILE: swap/client.go

DESCRIPTION:
Root client for the SWAP profile. Holds a reference to the parent okx.Client
(REST, Signer, Logger, Config) and exposes four domain sub-clients.

MAIN FUNCTIONS:
  - NewClient(parent)        : constructor. Registers with okx via init.
  - (*Client).Trading()      : domain sub-client for trading.
  - (*Client).Account()      : domain sub-client for positions/accounts.
  - (*Client).MarketData()   : domain sub-client for market data.
  - (*Client).Stream()       : domain sub-client for WS subscriptions.

CONTRACT:
  - Client is thread-safe: sub-clients are read-only after construction.
  - All REST calls go through parent.REST() — shared connection pool.

ID CONSTRAINTS:
  - clOrdId in OKX is limited to 32 characters [A-Za-z0-9] (case-sensitive
    alphanumerics, no underscores or punctuation); the `tag` field — 16 characters
    of the same alphabet. The SDK validates this at request-build time
    (see trading.go) and returns ErrorKindInvalidRequest without sending.
*/

package swap

import (
	"sync"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/ws"
)

// Client — OKX SWAP profile client.
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

// NewClient creates a SWAP client. The parent argument is required.
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

// Trading returns the trading sub-client.
func (c *Client) Trading() *TradingClient { return c.trading }

// Account returns the position/account sub-client.
func (c *Client) Account() *AccountClient { return c.account }

// MarketData returns the market data sub-client.
func (c *Client) MarketData() *MarketDataClient { return c.marketData }

// Stream returns the WS subscription sub-client.
func (c *Client) Stream() *StreamClient { return c.stream }

// logger / rest / signer — internal shortcuts for sub-clients.
func (c *Client) logger() okx.Logger  { return c.parent.Logger() }
func (c *Client) rest() restDoer      { return c.parent.REST() }
func (c *Client) config() okx.Config  { return c.parent.Config() }
func (c *Client) signerEnabled() bool { return c.parent.Signer().Enabled() }

// publicConn lazily creates and returns the public WS connection.
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

// privateConn lazily creates and returns the private WS connection.
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

// toWsConfig converts the public okx.WsConfig into a local ws.Config.
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

// init registers the factory in the root package so that okx.Client.Swap()
// lazily returns *swap.Client. This allows users to avoid an explicit swap import
// when working only through the root Client (though a blank-import of
// "github.com/tonymontanov/go-okx/v2/swap" is still required in that case).
func init() {
	okx.RegisterSwapFactory(func(parent *okx.Client) any {
		return NewClient(parent)
	})
}
