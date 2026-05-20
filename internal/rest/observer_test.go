/*
FILE: internal/rest/observer_test.go

DESCRIPTION:
Unit tests for the public RateLimitObserver contract (see okx.Config.RateLimitObserver
and rest.Config.RateLimitObserver). Tests use httptest.Server instead of the real
api.okx.com — giving full control over responses and headers without network access.

Coverage:
  - Observer is called exactly once per REST call;
  - receives the correct endpoint (opts.Path without query);
  - headers map is ALWAYS empty non-nil — even when the test server returns
    rate-limit headers, since v2.5.1 the SDK no longer reads them (OKX REST
    does not actually emit ratelimit-* headers, see internal/rest/client.go
    file header);
  - observer fires on HTTP errors too;
  - nil-observer is safe and does not cause a panic.
*/

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/okxlog"
)

// newObserverTestClient — rest.Client constructor with the given observer,
// no signing. Name differs from newTestClient (client_test.go) to avoid
// conflicts between tests in the same package.
func newObserverTestClient(t *testing.T, srv *httptest.Server, observer func(string, map[string]string)) *Client {
	t.Helper()
	return NewClient(
		srv.URL,
		auth.NewSigner("", "", ""),
		Config{RateLimitObserver: observer},
		"go-okx-test/2",
		okxlog.Noop(),
	)
}

// TestRateLimitObserver_CalledWithEndpointAndEmptyHeaders verifies the v2.5.1
// contract: even when the test server returns conventional ratelimit-* headers
// (mimicking what some other exchanges do), the SDK delivers an empty non-nil
// map to the observer. OKX itself never emits these headers in practice; the
// SDK intentionally does not forward them so that consumers do not develop a
// false dependency on a field that is permanently empty in prod.
func TestRateLimitObserver_CalledWithEndpointAndEmptyHeaders(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server returns ratelimit-* headers; SDK must still deliver an empty
		// map (post-v2.5.1 behaviour).
		w.Header().Set("ratelimit-limit", "60")
		w.Header().Set("ratelimit-remaining", "57")
		w.Header().Set("ratelimit-reset", "1")
		w.Header().Set("x-ratelimit-remaining", "57")
		w.Header().Set("OK-RateLimit-Remaining", "57")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":[]}`))
	}))
	defer srv.Close()

	var calls int32
	var gotEndpoint string
	var gotHeaders map[string]string
	var observer = func(endpoint string, headers map[string]string) {
		atomic.AddInt32(&calls, 1)
		gotEndpoint = endpoint
		gotHeaders = headers
	}

	var c *Client = newObserverTestClient(t, srv, observer)
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/api/v5/trade/order"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("observer calls = %d, want 1", got)
	}
	if gotEndpoint != "/api/v5/trade/order" {
		t.Fatalf("endpoint = %q, want /api/v5/trade/order", gotEndpoint)
	}
	if gotHeaders == nil {
		t.Fatal("headers must be non-nil")
	}
	if len(gotHeaders) != 0 {
		t.Fatalf("headers must be empty (server-emitted ratelimit-* must NOT leak through after v2.5.1), got %v", gotHeaders)
	}
}

// TestRateLimitObserver_CalledOnHTTPError — observer must fire even on 4xx/5xx
// responses so the rate-limiter strategy can react (back off, log, increment
// 50011/429 counters via Options.Meta etc.).
func TestRateLimitObserver_CalledOnHTTPError(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ratelimit-remaining", "0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"50011","msg":"Requests too frequent"}`))
	}))
	defer srv.Close()

	var calls int32
	var gotHeaders map[string]string
	var observer = func(endpoint string, headers map[string]string) {
		atomic.AddInt32(&calls, 1)
		gotHeaders = headers
	}

	var c *Client = newObserverTestClient(t, srv, observer)
	// Error is expected — but observer must be called before it.
	_, _, _ = c.Do(context.Background(), Options{Method: "POST", Path: "/api/v5/trade/order"})

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("observer calls = %d, want 1 (even on 429)", got)
	}
	if gotHeaders == nil {
		t.Fatal("headers must be non-nil on HTTP error too")
	}
	if len(gotHeaders) != 0 {
		t.Fatalf("headers must be empty on HTTP error too, got %v", gotHeaders)
	}
}

// TestRateLimitObserver_NonNilMapWhenNoHeaders — public docstring contract:
// when the server returns no headers at all (typical for OKX), the observer
// still receives an empty non-nil map.
func TestRateLimitObserver_NonNilMapWhenNoHeaders(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":[]}`))
	}))
	defer srv.Close()

	var gotHeaders map[string]string
	var sawCall bool
	var observer = func(endpoint string, headers map[string]string) {
		sawCall = true
		gotHeaders = headers
	}

	var c *Client = newObserverTestClient(t, srv, observer)
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/api/v5/public/time"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if !sawCall {
		t.Fatal("observer not called")
	}
	if gotHeaders == nil {
		t.Fatal("headers must be non-nil even when server returned no rate-limit headers")
	}
	if len(gotHeaders) != 0 {
		t.Fatalf("headers = %v, want empty map", gotHeaders)
	}
}

// TestRateLimitObserver_NilSafe — absence of an observer (default config) must
// not cause a panic. This is the basic backwards-compatibility guarantee with
// v2.0.x: users who do not set RateLimitObserver continue to work unchanged.
func TestRateLimitObserver_NilSafe(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":[]}`))
	}))
	defer srv.Close()

	var c *Client = newObserverTestClient(t, srv, nil)
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/api/v5/public/time"})
	if err != nil {
		t.Fatalf("nil observer must not affect Do: %v", err)
	}
}
