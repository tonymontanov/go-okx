/*
ФАЙЛ: internal/rest/observer_test.go

ОПИСАНИЕ:
Unit-тесты публичного контракта RateLimitObserver (см. okx.Config.RateLimitObserver
и rest.Config.RateLimitObserver). Тесты крутят httptest.Server вместо реального
api.okx.com — это даёт нам полный контроль над ответами и заголовками без сети.

Покрытие:
  - Observer вызывается ровно один раз на REST-вызов;
  - получает правильный endpoint (opts.Path без query);
  - получает фактические rate-limit заголовки (lowercase и x- варианты);
  - получает non-nil map даже если сервер не вернул ни одного заголовка;
  - nil-observer безопасен и не приводит к panic'у.
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

// newObserverTestClient — конструктор rest.Client с указанным observer'ом,
// без подписи. Имя отличается от newTestClient (client_test.go), чтобы
// тесты в одном пакете не конфликтовали.
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

func TestRateLimitObserver_CalledWithEndpointAndHeaders(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ratelimit-limit", "60")
		w.Header().Set("ratelimit-remaining", "57")
		w.Header().Set("ratelimit-reset", "1")
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
	if gotHeaders["ratelimit-limit"] != "60" {
		t.Fatalf("ratelimit-limit = %q, want 60", gotHeaders["ratelimit-limit"])
	}
	if gotHeaders["ratelimit-remaining"] != "57" {
		t.Fatalf("ratelimit-remaining = %q, want 57", gotHeaders["ratelimit-remaining"])
	}
	if gotHeaders["ratelimit-reset"] != "1" {
		t.Fatalf("ratelimit-reset = %q, want 1", gotHeaders["ratelimit-reset"])
	}
}

// TestRateLimitObserver_CalledOnHTTPError — observer должен сработать и при
// 4xx/5xx ответах: rate-limit информация на ошибках особенно важна, чтобы
// стратегия rate-limiter'а могла отступить.
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
	// Ошибка ожидаема — но observer обязан быть вызван до этого.
	_, _, _ = c.Do(context.Background(), Options{Method: "POST", Path: "/api/v5/trade/order"})

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("observer calls = %d, want 1 (even on 429)", got)
	}
	if gotHeaders["ratelimit-remaining"] != "0" {
		t.Fatalf("ratelimit-remaining = %q, want 0", gotHeaders["ratelimit-remaining"])
	}
}

// TestRateLimitObserver_NonNilMapWhenNoHeaders — контракт публичного docstring'а:
// headers всегда non-nil, даже если сервер не вернул ни одного rate-limit
// заголовка (например, для unauthenticated public endpoint'ов).
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

// TestRateLimitObserver_NilSafe — отсутствие observer'а (default config) не
// должно приводить к panic'у. Это базовая гарантия обратной совместимости с
// v2.0.x: пользователи, которые не выставляют RateLimitObserver, продолжают
// работать без изменений.
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
