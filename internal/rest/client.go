/*
FILE: internal/rest/client.go

DESCRIPTION:
Low-level SDK REST client. A thin layer over http.Client that:
  1. assembles the URL (BaseURL + path + query);
  2. signs the request (Signer);
  3. executes the HTTP call with deadline from ctx or Config.RequestTimeout;
  4. parses the OKX envelope {code, msg, data};
  5. maps errors to *okxerr.Error with the correct category;
  6. collects rate-limit headers to return to the caller.

IMPORT NOTE:
  - Does NOT import the root okx package (it imports rest), to avoid an
    import cycle. All required types (Error/ErrorKind/Logger/Config) live in
    internal/okxerr, internal/okxlog, and the local Config.
*/

package rest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/okxerr"
	"github.com/tonymontanov/go-okx/v2/internal/okxlog"
)

// rateLimitHeaders — OKX headers returned to the caller.
var rateLimitHeaders = []string{
	"ratelimit-limit",
	"ratelimit-remaining",
	"ratelimit-reset",
	"x-ratelimit-limit",
	"x-ratelimit-remaining",
	"x-ratelimit-reset",
}

// Config — REST transport parameters. Populated from the public okx.RestConfig
// in the root package (explicit struct conversion is done there to avoid an
// import cycle).
type Config struct {
	RequestTimeout      time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	// Demo — if true, every request gets the header
	// "x-simulated-trading: 1" — OKX switches processing to paper-trading mode.
	Demo bool
	// RateLimitObserver — optional legacy callback, called SYNCHRONOUSLY after
	// receiving the HTTP response (and before parsing the body) with the collected
	// rate-limit headers. nil → no-op. Full contract — see okx.Config in the root
	// package (where this field is published to the end SDK user).
	RateLimitObserver func(endpoint string, headers map[string]string)
	// RateLimitEventObserver — extended callback (v2.2.0+). Receives request
	// metadata (endpoint, method, headers, RequestMeta) that the root okx.Client
	// converts into the public okx.RateLimitEvent.
	// nil → no-op. If both observers are set, both are called in sequence.
	RateLimitEventObserver func(endpoint, method string, headers map[string]string, meta RequestMeta)
}

// RequestMeta — request metadata known at the domain layer
// (swap/trading.go, swap/account.go) that is needed by an external rate-limiter
// for accurate OKX limit tracking. Populated by the calling method and forwarded
// through rest.Options to RateLimitEventObserver. If empty, the observer
// receives zero values (count=0, no symbols, category="").
type RequestMeta struct {
	// OrderCount — number of orders affected by the request. 1 for single, N
	// for batch, 0 for non-trading. See okx.RateLimitEvent.OrderCount.
	OrderCount int
	// Symbols — list of OKX InstIDs. See okx.RateLimitEvent.Symbols.
	Symbols []string
	// Category — string representation of okx.RateLimitCategory
	// ("place"/"amend"/"cancel"/"query"/"market"/""). Passed as a string
	// to avoid an import cycle internal/rest ↔ root okx.
	Category string
}

// Options — parameters for a single REST request.
type Options struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
	Signed bool
	// Meta — metadata for RateLimitEventObserver. If zero, the observer receives
	// zeros. Populated by swap/* domain methods where instId / batch size /
	// request category are known.
	Meta RequestMeta
}

// Response — generic OKX response envelope:
//
//	{ "code":"0", "msg":"", "data":[...] }
type Response struct {
	Code string             `json:"code"`
	Msg  string             `json:"msg"`
	Data jsoniterRawMessage `json:"data"`
}

// jsoniterRawMessage — json.RawMessage equivalent that works correctly with jsoniter.
type jsoniterRawMessage []byte

// MarshalJSON implements json.Marshaler.
func (m jsoniterRawMessage) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("null"), nil
	}
	return []byte(m), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *jsoniterRawMessage) UnmarshalJSON(data []byte) error {
	*m = append((*m)[:0], data...)
	return nil
}

// UnmarshalData unmarshals the response data field into an arbitrary dest.
func (r Response) UnmarshalData(dest any) error {
	if len(r.Data) == 0 || bytes.Equal(r.Data, []byte("null")) {
		return nil
	}
	return codec.Unmarshal(r.Data, dest)
}

// Client — low-level REST client.
type Client struct {
	httpClient             *http.Client
	signer                 *auth.Signer
	baseURL                string
	userAgent              string
	logger                 okxlog.Logger
	demo                   bool
	rateLimitObserver      func(endpoint string, headers map[string]string)
	rateLimitEventObserver func(endpoint, method string, headers map[string]string, meta RequestMeta)
}

// NewClient creates a REST client.
func NewClient(baseURL string, signer *auth.Signer, cfg Config, ua string, log okxlog.Logger) *Client {
	if log == nil {
		log = okxlog.Noop()
	}
	var transport *http.Transport = &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		ForceAttemptHTTP2:   true,
	}
	var httpClient *http.Client = &http.Client{
		Timeout:   cfg.RequestTimeout,
		Transport: transport,
	}
	return &Client{
		httpClient:             httpClient,
		signer:                 signer,
		baseURL:                strings.TrimRight(baseURL, "/"),
		userAgent:              ua,
		logger:                 log,
		demo:                   cfg.Demo,
		rateLimitObserver:      cfg.RateLimitObserver,
		rateLimitEventObserver: cfg.RateLimitEventObserver,
	}
}

// Close closes idle transport connections.
func (c *Client) Close() {
	if c == nil || c.httpClient == nil {
		return
	}
	if t, ok := c.httpClient.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

/*
Do executes a single REST call and returns the Response envelope + rate-limit
headers + error. Error semantics — see package documentation.
*/
func (c *Client) Do(ctx context.Context, opts Options) (Response, map[string]string, error) {
	var resp Response
	var rateLimits map[string]string

	var fullURL string
	var bodyStr string
	var err error
	fullURL, bodyStr, err = c.buildRequest(opts)
	if err != nil {
		return resp, rateLimits, err
	}

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, strings.ToUpper(opts.Method), fullURL, bytes.NewBufferString(bodyStr))
	if err != nil {
		return resp, rateLimits, okxerr.New(okxerr.ErrorKindInvalidRequest, "", "rest: build request", err)
	}

	c.applyHeaders(req, opts, bodyStr)

	var httpResp *http.Response
	var started time.Time = time.Now()
	httpResp, err = c.httpClient.Do(req)
	if err != nil {
		return resp, rateLimits, classifyTransportError(err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	rateLimits = collectRateLimitHeaders(httpResp.Header)
	// Notify observers BEFORE parsing the body: even if the response is invalid
	// JSON or contains an OKX error, the rate-limit information is still useful
	// for the external rate-limiter (e.g. to avoid blocking a retry).
	// Non-nil map guarantee in observer — simplifies the subscriber (see okx.Config).
	//
	// If both observers are set, both are called in sequence. Order:
	// legacy (RateLimitObserver) first, then event (RateLimitEventObserver).
	// This enables gradual migration of existing subscribers without losing events
	// on either side.
	if c.rateLimitObserver != nil || c.rateLimitEventObserver != nil {
		var hdrs map[string]string = rateLimits
		if hdrs == nil {
			hdrs = map[string]string{}
		}
		if c.rateLimitObserver != nil {
			c.rateLimitObserver(opts.Path, hdrs)
		}
		if c.rateLimitEventObserver != nil {
			c.rateLimitEventObserver(opts.Path, strings.ToUpper(opts.Method), hdrs, opts.Meta)
		}
	}

	var raw []byte
	raw, err = io.ReadAll(httpResp.Body)
	if err != nil {
		return resp, rateLimits, okxerr.New(okxerr.ErrorKindNetwork, "", "rest: read body", err)
	}

	c.logger.Debug(
		"rest.Do",
		okxlog.Str("method", opts.Method),
		okxlog.Str("path", opts.Path),
		okxlog.Int("status", int64(httpResp.StatusCode)),
		okxlog.Int("durationMs", time.Since(started).Milliseconds()),
		okxlog.Int("bytes", int64(len(raw))),
	)

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		if err = codec.Unmarshal(raw, &resp); err != nil {
			return resp, rateLimits, okxerr.New(okxerr.ErrorKindUnknown, "", "rest: parse response", err)
		}
		// OKX top-level code semantics:
		//   "0"      — success;
		//   "1"      — bulk error: ALL data[] entries failed (see sCode/sMsg);
		//   "2"      — bulk partial: some data[] entries failed;
		//   other    — fatal at the request level (auth, rate-limit, validation).
		// For "1" and "2" we pass data up so the domain layer (Trading, etc.)
		// can extract per-entry sCode/sMsg and build a precise error. Without
		// this the user sees the useless "All operations failed".
		if resp.Code != "" && resp.Code != "0" && resp.Code != "1" && resp.Code != "2" {
			return resp, rateLimits, &okxerr.Error{
				Kind:       okxerr.MapOKXCode(resp.Code, resp.Msg),
				HTTPStatus: httpResp.StatusCode,
				OKXCode:    resp.Code,
				Message:    resp.Msg,
			}
		}
		return resp, rateLimits, nil
	}

	if err = codec.Unmarshal(raw, &resp); err == nil && resp.Code != "" {
		return resp, rateLimits, &okxerr.Error{
			Kind:       okxerr.MapOKXCode(resp.Code, resp.Msg),
			HTTPStatus: httpResp.StatusCode,
			OKXCode:    resp.Code,
			Message:    resp.Msg,
		}
	}
	return resp, rateLimits, &okxerr.Error{
		Kind:       okxerr.MapHTTPStatus(httpResp.StatusCode),
		HTTPStatus: httpResp.StatusCode,
		Message:    truncate(string(raw), 256),
	}
}

// buildRequest assembles the URL and serialized body.
func (c *Client) buildRequest(opts Options) (string, string, error) {
	var u *url.URL
	var err error
	u, err = url.Parse(c.baseURL + opts.Path)
	if err != nil {
		return "", "", okxerr.New(okxerr.ErrorKindInvalidRequest, "", "rest: invalid url", err)
	}
	if len(opts.Query) > 0 {
		u.RawQuery = opts.Query.Encode()
	}

	var body string
	if opts.Body != nil {
		var raw []byte
		raw, err = codec.Marshal(opts.Body)
		if err != nil {
			return "", "", okxerr.New(okxerr.ErrorKindInvalidRequest, "", "rest: marshal body", err)
		}
		body = string(raw)
	}
	return u.String(), body, nil
}

// applyHeaders sets standard and signed headers.
func (c *Client) applyHeaders(req *http.Request, opts Options, body string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if c.demo {
		req.Header.Set("x-simulated-trading", "1")
	}

	if !opts.Signed {
		return
	}
	if c.signer == nil || !c.signer.Enabled() {
		return
	}

	var ts string = c.signer.IsoTimestamp(time.Now())
	var signPath string = opts.Path
	if len(opts.Query) > 0 {
		signPath = signPath + "?" + opts.Query.Encode()
	}

	var signature string
	var err error
	signature, err = c.signer.Sign(ts, strings.ToUpper(opts.Method), signPath, body)
	if err != nil {
		c.logger.Warn("rest: sign skipped", okxlog.Err(err))
		return
	}
	req.Header.Set("OK-ACCESS-KEY", c.signer.APIKey())
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.signer.Passphrase())
}

// classifyTransportError converts a network/ctx error into a *okxerr.Error.
func classifyTransportError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return okxerr.New(okxerr.ErrorKindNetwork, "", "rest: context canceled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return okxerr.New(okxerr.ErrorKindNetwork, "", "rest: deadline exceeded", err)
	}
	return okxerr.New(okxerr.ErrorKindNetwork, "", "rest: transport error", err)
}

// collectRateLimitHeaders collects a fixed set of rate-limit headers.
func collectRateLimitHeaders(h http.Header) map[string]string {
	var out map[string]string
	var v string
	for _, key := range rateLimitHeaders {
		v = h.Get(key)
		if v == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string, 4)
		}
		out[key] = v
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
