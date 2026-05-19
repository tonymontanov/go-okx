/*
FILE: client.go

DESCRIPTION:
The main public SDK Client. Holds shared resources (REST client, signer,
config, logger) and provides lazy domain sub-clients on demand.
In v1 only the SWAP profile is supported; the SPOT profile is reserved and
implemented in a separate iteration.

MAIN FUNCTIONS:
  - NewClient(cfg)        : constructor with Config validation and defaults.
  - (Client).Swap()       : returns the SWAP-profile sub-client (instType=SWAP).
                            Created lazily on first access.
  - (Client).Close()      : gracefully shuts down background operations (WS
                            streams, connection pools). Non-blocking.

MAIN ENTITIES:
  - Client          : root SDK object.
  - swapClientCtor  : internal contract through which the swap package creates
                      its client. This avoids an import cycle between the root
                      (where Client lives) and swap (where SwapClient lives).

DEPENDENCIES:
- internal/auth, internal/rest: signing and REST transport.
- sync: lazy sub-client initialization.
*/

package okx

import (
	"sync"

	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/rest"
)

// Client — root SDK object.
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

// NewClient creates the root SDK client. cfg goes through withDefaults + validate.
// If credentials are set — the Signer will be enabled and sign private calls;
// otherwise the client can only access public endpoints.
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
	// Forward the new event-observer via a thin adapter. The RateLimitEvent
	// struct lives in the root okx package and CANNOT be passed directly into
	// internal/rest (import cycle). Therefore rest calls the callback with flat
	// arguments (endpoint, method, headers, meta), and here we assemble
	// RateLimitEvent for the final subscriber.
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

// Config returns a copy of the final config (after withDefaults). Useful for
// diagnostics and metrics.
func (c *Client) Config() Config { return c.cfg }

// Logger returns the current logger.
func (c *Client) Logger() Logger { return c.logger }

// Signer returns the internal/auth.Signer (for internal SDK sub-packages).
// Exported for use by swap/spot sub-packages — user code should not access the
// signer directly.
func (c *Client) Signer() *auth.Signer { return c.signer }

// REST returns the internal/rest.Client (for internal SDK sub-packages).
func (c *Client) REST() *rest.Client { return c.rest }

// Close releases resources (idle HTTP connections). Safe to call multiple times.
// WS streams terminate on cancellation of their contexts.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.rest.Close()
	return nil
}

// swapClientFactory — swap client builder function. Registered by the swap
// package via RegisterSwapFactory in init(). This avoids the import cycle.
var swapClientFactory func(c *Client) any

// RegisterSwapFactory registers the swap client factory. Must be called from
// the swap package's init(). Idempotent.
func RegisterSwapFactory(f func(c *Client) any) {
	if swapClientFactory == nil {
		swapClientFactory = f
	}
}

// Swap returns the swap sub-client. The return type is any because the root
// package cannot import swap (which imports the root). The caller immediately
// type-asserts to *swap.Client.
//
// Usage idiom:
//
//	var swapClient *swap.Client = client.Swap().(*swap.Client)
//
// Lazy: created on first access via the registered factory.
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

// spotClientFactory — spot client builder function. Registered by the spot
// package in init() exactly as swap (see RegisterSwapFactory).
var spotClientFactory func(c *Client) any

// RegisterSpotFactory registers the spot client factory. Must be called from
// the spot package's init(). Idempotent.
//
// Spot and Swap are independent domains: enabling one does NOT require the other.
// This lets applications import only the needed profile without pulling unused
// code into the binary:
//
//	import _ "github.com/tonymontanov/go-okx/v2/spot"  // spot only
//	import _ "github.com/tonymontanov/go-okx/v2/swap"  // swap only
//
// The root okx package does NOT import spot or swap — this avoids the import
// cycle (both packages import the root okx for okx.Config / okx.NewError etc.).
func RegisterSpotFactory(f func(c *Client) any) {
	if spotClientFactory == nil {
		spotClientFactory = f
	}
}

// Spot returns the spot sub-client. See Swap() — semantics are identical.
//
// Usage idiom:
//
//	var spotClient *spot.Client = client.Spot().(*spot.Client)
//
// Lazy: created on first access via the registered factory.
// If the spot package is not imported (factory not registered) —
// returns nil and logs a warning.
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
