/*
FILE: config.go

DESCRIPTION:
config.go defines the SDK configuration structs (see spec §5.5).
Also contains the typical production / demo OKX endpoints and default values.

MAIN FUNCTIONS:
  - DefaultConfig(): returns Config with production endpoints and default values
    for timeouts/reconnect/orderbook.
  - (Config).withDefaults(): fills empty Config fields with defaults. Inside
    the SDK the config is ALWAYS passed through withDefaults() first.

MAIN TYPES:
  - Config:                    public Client configuration.
  - RestConfig / WsConfig:     transport parameters (timeouts, reconnect, ping).
  - OrderbookConfig:           orderbook engine parameters (depth, resync policy).

ENDPOINTS:
Production OKX hosts are used by default:
  - REST: https://www.okx.com
  - WS public:   wss://ws.okx.com:8443/ws/v5/public
  - WS private:  wss://ws.okx.com:8443/ws/v5/private
  - WS business: wss://ws.okx.com:8443/ws/v5/business

DEPENDENCIES:
Standard library:
  - time: timeouts and reconnect/keepalive intervals.
*/

package okx

import "time"

// OKX transport URLs. Declared as vars rather than const so tests can override
// them (e.g. to point at a mock server).
var (
	// DefaultRestBaseURL — production REST endpoint OKX v5. Used for both
	// production and demo (demo only requires the x-simulated-trading: 1 header).
	DefaultRestBaseURL string = "https://www.okx.com"
	// DefaultWsPublicURL — production WS endpoint for public channels
	// (books/tickers/marks/index).
	DefaultWsPublicURL string = "wss://ws.okx.com:8443/ws/v5/public"
	// DefaultWsPrivateURL — production WS endpoint for private channels
	// (orders/positions/account).
	DefaultWsPrivateURL string = "wss://ws.okx.com:8443/ws/v5/private"
	// DefaultWsBusinessURL — production WS endpoint for business channels
	// (algo-orders, deposit-info, candles). Reserved for future use.
	DefaultWsBusinessURL string = "wss://ws.okx.com:8443/ws/v5/business"

	// DemoWsPublicURL — demo (paper-trading) WS endpoint for public channels.
	// OKX uses a dedicated host wspap.okx.com for demo.
	DemoWsPublicURL string = "wss://wspap.okx.com:8443/ws/v5/public"
	// DemoWsPrivateURL — demo WS endpoint for private channels.
	DemoWsPrivateURL string = "wss://wspap.okx.com:8443/ws/v5/private"
	// DemoWsBusinessURL — demo WS endpoint for business channels.
	DemoWsBusinessURL string = "wss://wspap.okx.com:8443/ws/v5/business"
)

// Config — public SDK configuration. Passed to NewClient.
type Config struct {
	// APIKey — OKX public key (OK-ACCESS-KEY).
	APIKey string
	// SecretKey — OKX secret key used for HMAC signing (OK-ACCESS-SIGN).
	SecretKey string
	// Passphrase — OKX mandatory passphrase (OK-ACCESS-PASSPHRASE).
	Passphrase string

	// REST — REST transport settings. If empty, DefaultConfig().REST is used.
	REST RestConfig
	// WS — WebSocket transport settings. If empty, DefaultConfig().WS is used.
	WS WsConfig
	// Orderbook — orderbook engine settings. If empty, DefaultConfig().Orderbook is used.
	Orderbook OrderbookConfig

	// Logger — optional logger. If nil, NoopLogger() is used.
	Logger Logger

	// Metrics — optional counter factory. If nil, NoopMetrics() is used.
	// The SDK creates the following counters through it (see docs):
	//   okx_ws_messages_received_total
	//   okx_ws_messages_dropped_total
	//   okx_ws_reconnects_total
	//   okx_ws_subscriptions_total
	//   okx_ws_ping_failed_total
	Metrics CounterFactory

	// UserAgent — User-Agent value for REST requests. Default: "go-okx/v2".
	UserAgent string

	// RateLimitObserver — optional hook called SYNCHRONOUSLY by the SDK after
	// every REST response (successful or OKX-level error) with the rate-limit
	// headers returned by OKX for the specific endpoint:
	//   ratelimit-limit / ratelimit-remaining / ratelimit-reset
	//   x-ratelimit-limit / x-ratelimit-remaining / x-ratelimit-reset
	//
	// Arguments:
	//   - endpoint: request path (e.g. "/api/v5/trade/batch-orders").
	//     OKX limits are per-endpoint, so this granularity is required.
	//   - headers:  actual headers returned by the server. May be an empty
	//     map if the endpoint does not return rate-limit info, but is always
	//     non-nil.
	//
	// The observer is NOT called on offline errors (timeout / network reset,
	// before an HTTP response arrives) because there is no new rate-limit
	// information in those cases. It is called on any received response,
	// including 4xx/5xx.
	//
	// Speed contract: the observer is called in the goroutine that executed
	// the REST call and blocks the return from Client.Do until it completes.
	// Implementations must be O(1) — typically a non-blocking send to a
	// buffered channel. Any blocking or panic stalls the caller's REST pipeline.
	//
	// If nil — no-op, zero overhead. This v2.x compatibility is guaranteed.
	//
	// Deprecated v2.2.0: prefer RateLimitEventObserver — it carries
	// OrderCount / Symbols / Category, without which correct modelling of
	// OKX rate limits is IMPOSSIBLE (see RateLimitEvent doc). The old observer
	// is kept only for backwards compatibility with v2.1.0; it will be removed in v3.
	RateLimitObserver func(endpoint string, headers map[string]string)

	// RateLimitEventObserver — extended observer added in v2.2.0.
	// Receives a structured RateLimitEvent on every REST response, including
	// fields without which an external rate-limiter CANNOT accurately model
	// OKX limits:
	//   - OrderCount: 1 for single endpoints, len(orders) for batch.
	//     OKX limits batch as "300 ORDERS per 2s", not "300 REQUESTS".
	//   - Symbols:   list of InstIDs the request applies to. OKX trading
	//     limits are per (UID + InstId), so the subscriber must see which
	//     symbols were consumed.
	//   - Category:  Place/Amend/Cancel/Query/Market — required for the
	//     sub-account-level plane (1000 new+amend orders / 2s, error 50061).
	//
	// Speed contract and nil semantics are identical to RateLimitObserver
	// (see above). If both observers are set, both are called sequentially
	// (legacy first, event second). This allows gradual migration of existing
	// subscribers without losing callbacks.
	//
	// If nil — no-op, zero overhead.
	RateLimitEventObserver func(RateLimitEvent)

	// Demo — switches the client to OKX Demo Trading mode (paper trading).
	// Effect:
	//   - REST: the header "x-simulated-trading: 1" is added to every request.
	//     The URL stays at production (DefaultRestBaseURL), because demo and prod
	//     share the same host — only the header distinguishes them.
	//   - WS:   public/private/business URLs are AUTOMATICALLY replaced with
	//     wspap.okx.com, unless the user set them explicitly via WS.PublicURL
	//     etc. If WS.PublicURL is already set (e.g. to a mock server), the SDK
	//     leaves it unchanged.
	//   - Separate keys are required: OKX demo and prod keys are NOT compatible;
	//     create demo keys at Profile → Demo Trading → API.
	Demo bool
}

// RestConfig — HTTP transport settings.
type RestConfig struct {
	// BaseURL — base URL for the OKX REST API. Default: DefaultRestBaseURL.
	BaseURL string
	// RequestTimeout — timeout for a single REST request. Default: 10s.
	// For latency-critical calls (place/cancel) pass a ctx with its own
	// deadline — it overrides RequestTimeout.
	RequestTimeout time.Duration
	// MaxIdleConns — idle connection pool size for http.Transport. Default: 100.
	MaxIdleConns int
	// MaxIdleConnsPerHost — pool size per host. Default: 100.
	MaxIdleConnsPerHost int
	// IdleConnTimeout — keep-alive idle timeout. Default: 90s.
	IdleConnTimeout time.Duration
}

// WsConfig — WebSocket transport settings.
type WsConfig struct {
	// PublicURL — public WS URL (orderbook/tickers/...). Default: DefaultWsPublicURL.
	PublicURL string
	// PrivateURL — private WS URL (orders/positions). Default: DefaultWsPrivateURL.
	PrivateURL string
	// BusinessURL — business WS URL. Default: DefaultWsBusinessURL.
	BusinessURL string
	// HandshakeTimeout — connection handshake timeout. Default: 10s.
	HandshakeTimeout time.Duration
	// ReadTimeout — read timeout for a single frame. Default: 35s (OKX sends ping every 30s).
	ReadTimeout time.Duration
	// WriteTimeout — write timeout for a single frame. Default: 5s.
	WriteTimeout time.Duration
	// PingInterval — client-side ping interval (OKX: text "ping" frame,
	// server replies "pong"). Default: 20s (OKX requirement <30s).
	PingInterval time.Duration
	// ReconnectInitialBackoff — initial delay between reconnect attempts. Default: 200ms.
	ReconnectInitialBackoff time.Duration
	// ReconnectMaxBackoff — upper bound of backoff. Default: 10s.
	ReconnectMaxBackoff time.Duration
	// ReconnectJitter — relative jitter [0..1] added to backoff. Default: 0.2.
	ReconnectJitter float64
	// ReadBufferSize — gorilla/websocket read buffer size. Default: 64KB.
	ReadBufferSize int
	// WriteBufferSize — gorilla/websocket write buffer size. Default: 16KB.
	WriteBufferSize int
}

// OrderbookConfig — orderbook engine settings.
type OrderbookConfig struct {
	// MaxDepth — depth of the local order book (number of levels per side).
	// Default: 400 (matches the OKX "books" channel).
	MaxDepth int
	// ChecksumLevels — number of levels OKX uses for CRC32. Always 25 per
	// specification. Parameterized in case the protocol changes.
	ChecksumLevels int
	// ChecksumMismatchToResync — how many consecutive CRC32 mismatches are
	// allowed before forcing a resync. Default: 1 (any mismatch triggers resync).
	ChecksumMismatchToResync int
}

// DefaultConfig returns a Config with all sensible defaults
// (production endpoints + production timeouts).
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

// withDefaults returns a Config where all empty fields are filled with values
// from DefaultConfig(). Used inside NewClient — the user-supplied Config is
// never mutated.
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

	// WS endpoints: for Demo use wspap.okx.com, for prod use ws.okx.com.
	// If the user explicitly set a URL — do NOT override it.
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

// validate checks that the required URL fields are set. Credentials are not
// enforced here because public REST/WS work without keys — the auth.Signer
// tracks a `signed` flag and enforces credentials at call time.
func (c Config) validate() error {
	if c.REST.BaseURL == "" {
		return NewError(ErrorKindInvalidRequest, "", "config: REST.BaseURL is empty", nil)
	}
	if c.WS.PublicURL == "" {
		return NewError(ErrorKindInvalidRequest, "", "config: WS.PublicURL is empty", nil)
	}
	return nil
}
