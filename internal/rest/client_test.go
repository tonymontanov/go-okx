/*
FILE: internal/rest/client_test.go

DESCRIPTION:
Low-level REST client tests focused on infrastructure aspects (not OKX domain
logic — that is covered in swap/contract_test.go).

COVERAGE:
  - TestDo_SetsDemoHeader: when cfg.Demo=true every request carries the
    "x-simulated-trading: 1" header.
  - TestDo_NoDemoHeaderByDefault: without Demo the header is absent.
  - TestDo_PassesThroughBulkCodes: top-level "code":"1"/"2" are not treated as
    fatal — the caller receives data to parse per-entry sCode.
*/

package rest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tonymontanov/go-okx/v2/internal/auth"
)

func newTestClient(t *testing.T, demo bool, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	var srv *httptest.Server = httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	var signer *auth.Signer = auth.NewSigner("k", "s", "p")
	var c *Client = NewClient(srv.URL, signer, Config{
		RequestTimeout: 2 * time.Second,
		Demo:           demo,
	}, "go-okx-test", nil)
	return c, srv
}

func TestDo_SetsDemoHeader(t *testing.T) {
	var seen string
	var c, _ = newTestClient(t, true, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("x-simulated-trading")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[]}`)
	})
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/api/v5/account/balance", Signed: true})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if seen != "1" {
		t.Fatalf("expected x-simulated-trading=1, got %q", seen)
	}
}

func TestDo_NoDemoHeaderByDefault(t *testing.T) {
	var seen string
	var c, _ = newTestClient(t, false, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("x-simulated-trading")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[]}`)
	})
	var _, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/api/v5/public/time", Signed: false})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if seen != "" {
		t.Fatalf("expected no x-simulated-trading header, got %q", seen)
	}
}

func TestDo_PassesThroughBulkCodes(t *testing.T) {
	var cases = []string{"1", "2"}
	var i int
	for i = 0; i < len(cases); i++ {
		var code string = cases[i]
		t.Run("code_"+code, func(t *testing.T) {
			var c, _ = newTestClient(t, false, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"code":"`+code+`","msg":"All operations failed","data":[{"sCode":"51000","sMsg":"detail"}]}`)
			})
			var resp Response
			var err error
			resp, _, err = c.Do(context.Background(), Options{Method: "POST", Path: "/api/v5/trade/batch-orders", Signed: true})
			if err != nil {
				t.Fatalf("bulk code %s must NOT be fatal at REST layer, got: %v", code, err)
			}
			if resp.Code != code {
				t.Fatalf("expected resp.Code=%s, got %q", code, resp.Code)
			}
			if len(resp.Data) == 0 {
				t.Fatal("expected data to pass through")
			}
		})
	}
}
