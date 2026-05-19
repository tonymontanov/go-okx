/*
FILE: swap/event_observer_test.go

DESCRIPTION:
End-to-end tests for the RateLimitEventObserver (v2.2.0+) at the domain-method
level of the swap package. internal/rest already covers the observer at the
Options level; here we verify that domain code sets the correct RequestMeta for
each method (OrderCount / Symbols / Category).

Method coverage:
  - Trading.CreateOrder       → single Place, 1 symbol
  - Trading.ModifyOrder       → single Amend, 1 symbol
  - Trading.CancelOrder       → single Cancel, 1 symbol
  - Trading.CreateBatchOrders → multi Place, OrderCount=len(orders),
                                Symbols=unique sorted set
  - Trading.ModifyBatchOrders → multi Amend
  - Trading.CancelBatchOrders → multi Cancel
  - Account.ClosePosition     → Place 1
  - Account.SetLeverage       → Query 0
  - Account.GetBalance        → Query 0, no symbols
  - Market.GetSymbolInfo      → Market 0, 1 symbol
*/

package swap

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// observedEvent — copy of RateLimitEvent for convenient test assertions.
type observedEvent struct {
	Endpoint   string
	Method     string
	OrderCount int
	Symbols    []string
	Category   okx.RateLimitCategory
}

// mockOKXWithEventObserver — same as mockOKX in contract_test.go, but configures
// an event-observer and returns a thread-safe collector.
func mockOKXWithEventObserver(t *testing.T, routes map[string]string) (*okx.Client, *eventCollector) {
	t.Helper()
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body string
		var ok bool
		body, ok = routes[r.URL.Path]
		if !ok {
			http.Error(w, `{"code":"404","msg":"no fixture","data":[]}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	var collector *eventCollector = &eventCollector{}
	var cfg okx.Config = okx.DefaultConfig()
	cfg.REST.BaseURL = srv.URL
	cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "k", "s", "p"
	cfg.REST.RequestTimeout = 3 * time.Second
	cfg.RateLimitEventObserver = collector.record

	var client *okx.Client
	var err error
	client, err = okx.NewClient(cfg)
	if err != nil {
		t.Fatalf("okx.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, collector
}

type eventCollector struct {
	mu     sync.Mutex
	events []observedEvent
}

func (c *eventCollector) record(ev okx.RateLimitEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, observedEvent{
		Endpoint:   ev.Endpoint,
		Method:     ev.Method,
		OrderCount: ev.OrderCount,
		Symbols:    append([]string(nil), ev.Symbols...),
		Category:   ev.Category,
	})
}

func (c *eventCollector) last(t *testing.T) observedEvent {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		t.Fatal("no observer events captured")
	}
	return c.events[len(c.events)-1]
}

func TestEventObserver_CreateOrder_SingleSymbol(t *testing.T) {
	var fixture string = `{"code":"0","msg":"","data":[{"clOrdId":"abc","ordId":"1","sCode":"0","sMsg":""}]}`
	var client, obs = mockOKXWithEventObserver(t, map[string]string{
		"/api/v5/trade/order": fixture,
	})
	var _, err = swapOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:    "BTC-USDT-SWAP",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeLimit,
		Price:     mustDec("40000"),
		Size:      mustDec("1"),
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	var got observedEvent = obs.last(t)
	var want observedEvent = observedEvent{
		Endpoint:   "/api/v5/trade/order",
		Method:     "POST",
		OrderCount: 1,
		Symbols:    []string{"BTC-USDT-SWAP"},
		Category:   okx.RateLimitCategoryPlace,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event = %+v, want %+v", got, want)
	}
}

func TestEventObserver_CreateBatchOrders_MultiSymbol(t *testing.T) {
	var fixture string = `{"code":"0","msg":"","data":[
		{"clOrdId":"a","ordId":"1","sCode":"0"},
		{"clOrdId":"b","ordId":"2","sCode":"0"},
		{"clOrdId":"c","ordId":"3","sCode":"0"}
	]}`
	var client, obs = mockOKXWithEventObserver(t, map[string]string{
		"/api/v5/trade/batch-orders": fixture,
	})
	// 2 BTC + 1 ETH = 3 orders, 2 unique symbols. Order of the original
	// batch is intentionally shuffled — verify that Symbols is sorted.
	var reqs []types.CreateOrderRequest = []types.CreateOrderRequest{
		{InstID: "ETH-USDT-SWAP", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("2500"), Size: mustDec("1")},
		{InstID: "BTC-USDT-SWAP", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("40000"), Size: mustDec("1")},
		{InstID: "BTC-USDT-SWAP", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("40100"), Size: mustDec("1")},
	}
	var _, err = swapOf(client).Trading().CreateBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("CreateBatchOrders: %v", err)
	}

	var got observedEvent = obs.last(t)
	var want observedEvent = observedEvent{
		Endpoint:   "/api/v5/trade/batch-orders",
		Method:     "POST",
		OrderCount: 3,
		Symbols:    []string{"BTC-USDT-SWAP", "ETH-USDT-SWAP"},
		Category:   okx.RateLimitCategoryPlace,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event = %+v, want %+v", got, want)
	}
}

func TestEventObserver_ModifyAndCancelCategories(t *testing.T) {
	var fixture string = `{"code":"0","msg":"","data":[{"ordId":"1","sCode":"0"}]}`
	var client, obs = mockOKXWithEventObserver(t, map[string]string{
		"/api/v5/trade/amend-order":  fixture,
		"/api/v5/trade/cancel-order": fixture,
	})

	var _, err = swapOf(client).Trading().ModifyOrder(context.Background(), types.ModifyOrderRequest{
		InstID:   "BTC-USDT-SWAP",
		OrderID:  "1",
		NewPrice: mustDec("40100"),
	})
	if err != nil {
		t.Fatalf("ModifyOrder: %v", err)
	}
	var amend observedEvent = obs.last(t)
	if amend.Category != okx.RateLimitCategoryAmend {
		t.Errorf("ModifyOrder Category = %q, want amend", amend.Category)
	}
	if amend.OrderCount != 1 {
		t.Errorf("ModifyOrder OrderCount = %d, want 1", amend.OrderCount)
	}

	err = swapOf(client).Trading().CancelOrder(context.Background(), types.CancelOrderRequest{
		InstID:  "BTC-USDT-SWAP",
		OrderID: "1",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	var cancel observedEvent = obs.last(t)
	if cancel.Category != okx.RateLimitCategoryCancel {
		t.Errorf("CancelOrder Category = %q, want cancel", cancel.Category)
	}
	if cancel.OrderCount != 1 {
		t.Errorf("CancelOrder OrderCount = %d, want 1", cancel.OrderCount)
	}
}

func TestEventObserver_AccountAndMarketCategories(t *testing.T) {
	var positionsFixture string = `{"code":"0","msg":"","data":[]}`
	var balanceFixture string = `{"code":"0","msg":"","data":[]}`
	var instrumentsFixture string = `{"code":"0","msg":"","data":[{"instId":"BTC-USDT-SWAP","baseCcy":"","quoteCcy":"","settleCcy":"USDT","ctVal":"0.01","ctMult":"1","tickSz":"0.1","lotSz":"1","minSz":"1","maxLmtSz":"100000","maxMktSz":"12000"}]}`
	var setLeverageFixture string = `{"code":"0","msg":"","data":[]}`
	var client, obs = mockOKXWithEventObserver(t, map[string]string{
		"/api/v5/account/positions":    positionsFixture,
		"/api/v5/account/balance":      balanceFixture,
		"/api/v5/public/instruments":   instrumentsFixture,
		"/api/v5/account/set-leverage": setLeverageFixture,
	})

	// Account.GetBalance: Query, no symbols
	var _, err = swapOf(client).Account().GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	var ev observedEvent = obs.last(t)
	if ev.Category != okx.RateLimitCategoryQuery {
		t.Errorf("GetBalance Category = %q, want query", ev.Category)
	}
	if len(ev.Symbols) != 0 {
		t.Errorf("GetBalance Symbols = %v, want empty", ev.Symbols)
	}

	// Account.GetPositions (via GetSymbolPosition): Query, 1 symbol
	_, err = swapOf(client).Account().GetSymbolPosition(context.Background(), "BTC-USDT-SWAP")
	if err != nil {
		t.Fatalf("GetSymbolPosition: %v", err)
	}
	ev = obs.last(t)
	if ev.Category != okx.RateLimitCategoryQuery {
		t.Errorf("GetPositions Category = %q, want query", ev.Category)
	}
	if !reflect.DeepEqual(ev.Symbols, []string{"BTC-USDT-SWAP"}) {
		t.Errorf("GetPositions Symbols = %v, want [BTC-USDT-SWAP]", ev.Symbols)
	}

	// Market.GetSymbolInfo: Market, 1 symbol
	_, err = swapOf(client).MarketData().GetSymbolInfo(context.Background(), "BTC-USDT-SWAP")
	if err != nil {
		t.Fatalf("GetSymbolInfo: %v", err)
	}
	ev = obs.last(t)
	if ev.Category != okx.RateLimitCategoryMarketData {
		t.Errorf("GetSymbolInfo Category = %q, want market", ev.Category)
	}

	// SetLeverage: Query (account-config POST), 1 symbol
	err = swapOf(client).Account().SetLeverage(context.Background(), "BTC-USDT-SWAP", 5)
	if err != nil {
		t.Fatalf("SetLeverage: %v", err)
	}
	ev = obs.last(t)
	if ev.Category != okx.RateLimitCategoryQuery {
		t.Errorf("SetLeverage Category = %q, want query", ev.Category)
	}
	if ev.OrderCount != 0 {
		t.Errorf("SetLeverage OrderCount = %d, want 0", ev.OrderCount)
	}
}

func TestEventObserver_ClosePosition_TreatedAsPlace(t *testing.T) {
	var fixture string = `{"code":"0","msg":"","data":[{"instId":"BTC-USDT-SWAP","posSide":"net"}]}`
	var client, obs = mockOKXWithEventObserver(t, map[string]string{
		"/api/v5/trade/close-position": fixture,
	})
	var err = swapOf(client).Account().ClosePosition(context.Background(), "BTC-USDT-SWAP")
	if err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}
	var ev observedEvent = obs.last(t)
	// ClosePosition sends a market-order — counted as Place (sub-account 50061
	// counter sees it; per-symbol rate-limit too).
	if ev.Category != okx.RateLimitCategoryPlace {
		t.Errorf("ClosePosition Category = %q, want place", ev.Category)
	}
	if ev.OrderCount != 1 {
		t.Errorf("ClosePosition OrderCount = %d, want 1", ev.OrderCount)
	}
	if !reflect.DeepEqual(ev.Symbols, []string{"BTC-USDT-SWAP"}) {
		t.Errorf("ClosePosition Symbols = %v", ev.Symbols)
	}
}
