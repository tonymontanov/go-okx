/*
ФАЙЛ: spot/event_observer_test.go

ОПИСАНИЕ:
End-to-end тесты на RateLimitEventObserver для SPOT-домена. Проверяем что
доменные методы корректно проставляют RequestMeta (OrderCount/Symbols/Category)
для каждого endpoint'а — без этого внешний rate-limiter не сможет моделировать
лимиты OKX точно.

Покрытие методов:
  - Trading.CreateOrder       → Place, 1 symbol, OrderCount=1
  - Trading.CreateBatchOrders → Place, multi symbols, OrderCount=len(orders)
  - Trading.ModifyOrder       → Amend, 1 symbol
  - Trading.CancelOrder       → Cancel, 1 symbol
  - Account.GetBalance        → Query, no symbols
  - Account.GetOpenOrders     → Query, 1 symbol
  - Market.GetSymbolInfo      → Market, 1 symbol
*/

package spot

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
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

type observedEvent struct {
	Endpoint   string
	Method     string
	OrderCount int
	Symbols    []string
	Category   okx.RateLimitCategory
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

func TestEventObserver_CreateOrder_SingleSymbol(t *testing.T) {
	var fixture string = `{"code":"0","msg":"","data":[{"clOrdId":"abc","ordId":"1","sCode":"0"}]}`
	var client, obs = mockOKXWithEventObserver(t, map[string]string{
		"/api/v5/trade/order": fixture,
	})
	var _, err = spotOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:    "BTC-USDT",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeLimit,
		Price:     mustDec("40000"),
		Size:      mustDec("0.1"),
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	var got observedEvent = obs.last(t)
	var want observedEvent = observedEvent{
		Endpoint:   "/api/v5/trade/order",
		Method:     "POST",
		OrderCount: 1,
		Symbols:    []string{"BTC-USDT"},
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
	var reqs []types.CreateOrderRequest = []types.CreateOrderRequest{
		{InstID: "ETH-USDT", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("2500"), Size: mustDec("1")},
		{InstID: "BTC-USDT", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("40000"), Size: mustDec("0.1")},
		{InstID: "BTC-USDT", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("40100"), Size: mustDec("0.1")},
	}
	var _, err = spotOf(client).Trading().CreateBatchOrders(context.Background(), reqs)
	if err != nil {
		t.Fatalf("CreateBatchOrders: %v", err)
	}

	var got observedEvent = obs.last(t)
	var want observedEvent = observedEvent{
		Endpoint:   "/api/v5/trade/batch-orders",
		Method:     "POST",
		OrderCount: 3,
		Symbols:    []string{"BTC-USDT", "ETH-USDT"},
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

	var _, err = spotOf(client).Trading().ModifyOrder(context.Background(), types.ModifyOrderRequest{
		InstID:   "BTC-USDT",
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
	if !reflect.DeepEqual(amend.Symbols, []string{"BTC-USDT"}) {
		t.Errorf("ModifyOrder Symbols = %v", amend.Symbols)
	}

	err = spotOf(client).Trading().CancelOrder(context.Background(), types.CancelOrderRequest{
		InstID:  "BTC-USDT",
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
	var balanceFixture string = `{"code":"0","msg":"","data":[]}`
	var ordersFixture string = `{"code":"0","msg":"","data":[]}`
	var instrumentsFixture string = `{"code":"0","msg":"","data":[{"instId":"BTC-USDT","baseCcy":"BTC","quoteCcy":"USDT","tickSz":"0.1","lotSz":"0.00001","minSz":"0.00001","maxLmtSz":"9999999","maxMktSz":"1000000"}]}`
	var client, obs = mockOKXWithEventObserver(t, map[string]string{
		"/api/v5/account/balance":      balanceFixture,
		"/api/v5/trade/orders-pending": ordersFixture,
		"/api/v5/public/instruments":   instrumentsFixture,
	})

	// GetBalance: Query, без symbols
	var _, err = spotOf(client).Account().GetBalance(context.Background())
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
	if ev.OrderCount != 0 {
		t.Errorf("GetBalance OrderCount = %d, want 0", ev.OrderCount)
	}

	// GetOpenOrders: Query, 1 symbol
	_, err = spotOf(client).Account().GetOpenOrders(context.Background(), "BTC-USDT")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	ev = obs.last(t)
	if ev.Category != okx.RateLimitCategoryQuery {
		t.Errorf("GetOpenOrders Category = %q, want query", ev.Category)
	}
	if !reflect.DeepEqual(ev.Symbols, []string{"BTC-USDT"}) {
		t.Errorf("GetOpenOrders Symbols = %v", ev.Symbols)
	}

	// GetSymbolInfo: Market, 1 symbol
	_, err = spotOf(client).MarketData().GetSymbolInfo(context.Background(), "BTC-USDT")
	if err != nil {
		t.Fatalf("GetSymbolInfo: %v", err)
	}
	ev = obs.last(t)
	if ev.Category != okx.RateLimitCategoryMarketData {
		t.Errorf("GetSymbolInfo Category = %q, want market", ev.Category)
	}
	if !reflect.DeepEqual(ev.Symbols, []string{"BTC-USDT"}) {
		t.Errorf("GetSymbolInfo Symbols = %v", ev.Symbols)
	}
}
