/*
FILE: internal/rest/event_observer_test.go

DESCRIPTION:
Unit tests for the extended rate-limit observer (v2.2.0+). The old observer is
covered by observer_test.go; here — only the new event-observer and the contract
for two-observer coexistence.

Coverage:
  - event-observer is called with the correct (endpoint, method, headers, meta);
  - OrderCount / Symbols / Category are forwarded exactly as set by the domain method;
  - meta = zero value for requests without options;
  - if both observers are set — both are called (legacy first, event second);
  - nil event-observer is safe.
*/

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/okxlog"
)

// newEventObserverTestClient creates a rest.Client with the given event-observer
// and an optional legacy observer — for coexistence tests.
func newEventObserverTestClient(
	t *testing.T,
	srv *httptest.Server,
	legacy func(string, map[string]string),
	event func(string, string, map[string]string, RequestMeta),
) *Client {
	t.Helper()
	return NewClient(
		srv.URL,
		auth.NewSigner("", "", ""),
		Config{
			RateLimitObserver:      legacy,
			RateLimitEventObserver: event,
		},
		"go-okx-test/2",
		okxlog.Noop(),
	)
}

func TestRateLimitEventObserver_CarriesMetaForBatch(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":[]}`))
	}))
	defer srv.Close()

	var calls int32
	var gotEndpoint string
	var gotMethod string
	var gotMeta RequestMeta
	var event = func(endpoint, method string, headers map[string]string, meta RequestMeta) {
		atomic.AddInt32(&calls, 1)
		gotEndpoint = endpoint
		gotMethod = method
		gotMeta = meta
		if headers == nil {
			t.Errorf("headers must be non-nil in event observer")
		}
	}

	var c *Client = newEventObserverTestClient(t, srv, nil, event)
	var _, _, err = c.Do(context.Background(), Options{
		Method: "post",
		Path:   "/api/v5/trade/batch-orders",
		Meta: RequestMeta{
			OrderCount: 17,
			Symbols:    []string{"BTC-USDT-SWAP", "ETH-USDT-SWAP"},
			Category:   "place",
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("event observer calls = %d, want 1", got)
	}
	if gotEndpoint != "/api/v5/trade/batch-orders" {
		t.Fatalf("endpoint = %q", gotEndpoint)
	}
	if gotMethod != "POST" {
		// event-observer always normalizes method to UPPER — this is the contract.
		t.Fatalf("method = %q, want POST (uppercased)", gotMethod)
	}
	if gotMeta.OrderCount != 17 {
		t.Fatalf("meta.OrderCount = %d, want 17", gotMeta.OrderCount)
	}
	if !reflect.DeepEqual(gotMeta.Symbols, []string{"BTC-USDT-SWAP", "ETH-USDT-SWAP"}) {
		t.Fatalf("meta.Symbols = %v", gotMeta.Symbols)
	}
	if gotMeta.Category != "place" {
		t.Fatalf("meta.Category = %q, want place", gotMeta.Category)
	}
}

func TestRateLimitEventObserver_ZeroMetaForUnannotatedRequest(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":[]}`))
	}))
	defer srv.Close()

	var gotMeta RequestMeta
	var event = func(endpoint, method string, headers map[string]string, meta RequestMeta) {
		gotMeta = meta
	}

	var c *Client = newEventObserverTestClient(t, srv, nil, event)
	// Request without Meta — observer must receive zero value, not panic.
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/api/v5/public/time"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if gotMeta.OrderCount != 0 {
		t.Fatalf("zero-meta OrderCount = %d, want 0", gotMeta.OrderCount)
	}
	if gotMeta.Symbols != nil {
		t.Fatalf("zero-meta Symbols = %v, want nil", gotMeta.Symbols)
	}
	if gotMeta.Category != "" {
		t.Fatalf("zero-meta Category = %q, want \"\"", gotMeta.Category)
	}
}

// TestRateLimitEventObserver_BothObserversFireInOrder — critical contract:
// if the user is subscribed to both legacy and event observer (needed during
// migration), both must be called. This provides correct backwards-compat:
// existing subscribers continue to work unchanged and can migrate to the
// event API gradually.
func TestRateLimitEventObserver_BothObserversFireInOrder(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ratelimit-remaining", "42")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":[]}`))
	}))
	defer srv.Close()

	var sequence []string
	var legacy = func(endpoint string, headers map[string]string) {
		sequence = append(sequence, "legacy:"+endpoint+":"+headers["ratelimit-remaining"])
	}
	var event = func(endpoint, method string, headers map[string]string, meta RequestMeta) {
		sequence = append(sequence, "event:"+endpoint+":"+headers["ratelimit-remaining"])
	}

	var c *Client = newEventObserverTestClient(t, srv, legacy, event)
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/api/v5/trade/orders-pending"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var want []string = []string{
		"legacy:/api/v5/trade/orders-pending:42",
		"event:/api/v5/trade/orders-pending:42",
	}
	if !reflect.DeepEqual(sequence, want) {
		t.Fatalf("observer call sequence = %v, want %v", sequence, want)
	}
}

// TestRateLimitEventObserver_NilSafe — nil event-observer must not affect
// the client (including the case when a legacy observer is set).
func TestRateLimitEventObserver_NilSafe(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":[]}`))
	}))
	defer srv.Close()

	var legacyCalled bool
	var legacy = func(endpoint string, headers map[string]string) {
		legacyCalled = true
	}
	var c *Client = newEventObserverTestClient(t, srv, legacy, nil)
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/api/v5/public/time"})
	if err != nil {
		t.Fatalf("nil event observer must not affect Do: %v", err)
	}
	if !legacyCalled {
		t.Fatal("legacy observer must still fire when event observer is nil")
	}
}
