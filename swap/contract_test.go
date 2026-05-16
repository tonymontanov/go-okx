/*
ФАЙЛ: swap/contract_test.go

ОПИСАНИЕ:
Contract-тесты swap-клиента (§13 ТЗ). Они проверяют, что наш парсер корректно
маппит реальные JSON-ответы OKX v5 в доменные структуры. Эти фикстуры
скопированы из официальной документации OKX (api/v5) и из реальных ответов
песочницы.

Покрытие:
  - GetSymbolInfo:           /api/v5/public/instruments (instType=SWAP)
  - GetOrderBook:            /api/v5/market/books
  - GetHistoricalCandles:    /api/v5/market/history-candles
  - GetPositions:            /api/v5/account/positions
  - GetOpenOrders:           /api/v5/trade/orders-pending
  - CreateOrder happy-path:  /api/v5/trade/order
  - CreateOrder reject:      sCode != "0" → возвращаем *okx.Error
  - CancelOrder:             /api/v5/trade/cancel-order
  - rate-limit headers пробрасываются в OrderInfo.RateLimits
  - OKX-уровневая ошибка с code != "0" маппится в *okx.Error.OKXCode

Тесты используют локальный httptest.Server, никаких походов в сеть.
*/

package swap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// mustDec — хелпер для тестов, парсит строку в decimal.Decimal или фейлит.
func mustDec(s string) decimal.Decimal {
	var d decimal.Decimal
	var err error
	d, err = decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// mockOKX поднимает httptest.Server, маршрутизирующий запросы по path и
// возвращающий заранее заготовленный JSON. Все остальные path возвращают 404
// чтобы тест явно падал, если запрос ушёл не туда.
func mockOKX(t *testing.T, routes map[string]string) (*httptest.Server, *okx.Client) {
	t.Helper()

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body string
		var ok bool
		body, ok = routes[r.URL.Path]
		if !ok {
			http.Error(w, `{"code":"404","msg":"no fixture for `+r.URL.Path+`","data":[]}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ratelimit-limit", "60")
		w.Header().Set("ratelimit-remaining", "59")
		w.Header().Set("ratelimit-reset", "1")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	var cfg okx.Config = okx.DefaultConfig()
	cfg.REST.BaseURL = srv.URL
	cfg.APIKey = "k"
	cfg.SecretKey = "s"
	cfg.Passphrase = "p"
	cfg.REST.RequestTimeout = 3 * time.Second

	var client *okx.Client
	var err error
	client, err = okx.NewClient(cfg)
	if err != nil {
		t.Fatalf("okx.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return srv, client
}

func swapOf(c *okx.Client) *Client { return c.Swap().(*Client) }

func TestContract_GetSymbolInfo(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"instId":"BTC-USDT-SWAP","baseCcy":"","quoteCcy":"",
			"settleCcy":"USDT","ctVal":"0.01","ctMult":"1",
			"tickSz":"0.1","lotSz":"1","minSz":"1",
			"maxLmtSz":"100000","maxMktSz":"12000"
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/public/instruments": fixture,
	})
	var info types.SymbolInfo
	var err error
	info, err = swapOf(client).MarketData().GetSymbolInfo(context.Background(), "BTC-USDT-SWAP")
	if err != nil {
		t.Fatalf("GetSymbolInfo: %v", err)
	}
	if info.InstID != "BTC-USDT-SWAP" {
		t.Fatalf("InstID: got %q", info.InstID)
	}
	if info.SettleCcy != "USDT" {
		t.Fatalf("SettleCcy: got %q", info.SettleCcy)
	}
	if !info.TickSize.Equal(mustDec("0.1")) {
		t.Fatalf("TickSize: got %v", info.TickSize)
	}
	if !info.LotSize.Equal(mustDec("1")) {
		t.Fatalf("LotSize: got %v", info.LotSize)
	}
	if info.PricePrecision != 1 {
		t.Fatalf("PricePrecision: got %d", info.PricePrecision)
	}
	if info.QuantityPrecision != 0 {
		t.Fatalf("QuantityPrecision: got %d", info.QuantityPrecision)
	}
}

func TestContract_GetSymbolInfo_NotFound(t *testing.T) {
	var fixture string = `{"code":"0","msg":"","data":[]}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/public/instruments": fixture,
	})
	var _, err = swapOf(client).MarketData().GetSymbolInfo(context.Background(), "FOO-BAR-SWAP")
	if !okx.IsInvalidRequest(err) {
		t.Fatalf("expected ErrorKindInvalidRequest, got %v", err)
	}
}

func TestContract_GetOrderBook(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"asks":[["41006.8","0.60030921","0","1"],["41007.1","0.5","0","1"]],
			"bids":[["41006.3","0.30178218","0","2"],["41006.2","1","0","1"]],
			"ts":"1629966436396"
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/market/books": fixture,
	})
	var ob types.OrderBookSnapshot
	var err error
	ob, err = swapOf(client).MarketData().GetOrderBook(context.Background(), "BTC-USDT-SWAP", 20)
	if err != nil {
		t.Fatalf("GetOrderBook: %v", err)
	}
	if len(ob.Asks) != 2 || len(ob.Bids) != 2 {
		t.Fatalf("levels count: asks=%d bids=%d", len(ob.Asks), len(ob.Bids))
	}
	if !ob.Asks[0].Price.Equal(mustDec("41006.8")) || !ob.Asks[0].Size.Equal(mustDec("0.60030921")) {
		t.Fatalf("ask[0]: got price=%v size=%v", ob.Asks[0].Price, ob.Asks[0].Size)
	}
	if !ob.Bids[0].Price.Equal(mustDec("41006.3")) {
		t.Fatalf("bid[0].Price: got %v", ob.Bids[0].Price)
	}
	if ob.Ts != 1629966436396 {
		t.Fatalf("Ts: got %d", ob.Ts)
	}
}

func TestContract_GetHistoricalCandles(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[
			["1597026383085","3.721","3.743","3.677","3.708","8422410","22698348.04828491","0","1"],
			["1597026323085","3.732","3.760","3.715","3.721","12238120","32915004.31511591","0","1"]
		]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/market/history-candles": fixture,
	})
	var candles types.Candles
	var err error
	candles, err = swapOf(client).MarketData().GetHistoricalCandles(context.Background(), "BTC-USDT-SWAP", types.Timeframe1m, 2)
	if err != nil {
		t.Fatalf("GetHistoricalCandles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len: got %d", len(candles))
	}
	if candles[0].OpenTimeMs != 1597026383085 {
		t.Fatalf("OpenTimeMs[0]: got %d", candles[0].OpenTimeMs)
	}
	if !candles[0].Open.Equal(mustDec("3.721")) {
		t.Fatalf("Open[0]: got %v", candles[0].Open)
	}
	if !candles[0].Closed {
		t.Fatalf("Closed[0] must be true")
	}
}

func TestContract_GetHistoricalCandles_SubMinute(t *testing.T) {
	var _, client = mockOKX(t, map[string]string{})
	var _, err = swapOf(client).MarketData().GetHistoricalCandles(context.Background(), "BTC-USDT-SWAP", types.Timeframe1s, 10)
	if !okx.IsInvalidRequest(err) {
		t.Fatalf("expected InvalidRequest, got %v", err)
	}
}

func TestContract_GetBalance(t *testing.T) {
	// Сокращённая, но реалистичная фикстура из OKX docs v5 (Get Balance).
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"uTime":"1614847029331",
			"totalEq":"91884",
			"isoEq":"0",
			"adjEq":"91884",
			"ordFroz":"0",
			"imr":"0",
			"mmr":"0",
			"mgnRatio":"99999",
			"notionalUsd":"0",
			"details":[
				{"ccy":"USDT","eq":"91884","cashBal":"91884","isoEq":"0","availEq":"91884",
				 "disEq":"91884","availBal":"91884","frozenBal":"0","ordFrozen":"0",
				 "upl":"0","isoUpl":"0","mgnRatio":"99999","eqUsd":"91884",
				 "uTime":"1614847029331"},
				{"ccy":"BTC","eq":"0.5","cashBal":"0.5","isoEq":"0","availEq":"0.5",
				 "disEq":"22000","availBal":"0.5","frozenBal":"0","ordFrozen":"0",
				 "upl":"0","isoUpl":"0","mgnRatio":"99999","eqUsd":"22000",
				 "uTime":"1614847029331"}
			]
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/account/balance": fixture,
	})
	var bal types.Balance
	var err error
	bal, err = swapOf(client).Account().GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if !bal.TotalEquityUSD.Equal(mustDec("91884")) {
		t.Fatalf("TotalEquityUSD: got %v", bal.TotalEquityUSD)
	}
	if !bal.AdjustedEquityUSD.Equal(mustDec("91884")) {
		t.Fatalf("AdjustedEquityUSD: got %v", bal.AdjustedEquityUSD)
	}
	if !bal.MarginRatio.Equal(mustDec("99999")) {
		t.Fatalf("MarginRatio: got %v", bal.MarginRatio)
	}
	if bal.UpdatedAtMs != 1614847029331 {
		t.Fatalf("UpdatedAtMs: got %d", bal.UpdatedAtMs)
	}
	if len(bal.Details) != 2 {
		t.Fatalf("details: got %d", len(bal.Details))
	}
	// проверим, что per-currency маппинг работает корректно
	var usdt *types.BalanceDetail
	var i int
	for i = 0; i < len(bal.Details); i++ {
		if bal.Details[i].Ccy == "USDT" {
			usdt = &bal.Details[i]
			break
		}
	}
	if usdt == nil {
		t.Fatal("USDT detail missing")
	}
	if !usdt.AvailableEquity.Equal(mustDec("91884")) {
		t.Fatalf("USDT.AvailableEquity: got %v", usdt.AvailableEquity)
	}
	if !usdt.EquityUSD.Equal(mustDec("91884")) {
		t.Fatalf("USDT.EquityUSD: got %v", usdt.EquityUSD)
	}
}

func TestContract_GetBalance_FilteredCcy(t *testing.T) {
	// Проверяем, что параметр ccy=BTC,USDT уходит в query.
	var seen string
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query().Get("ccy")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[{"totalEq":"0","details":[]}]}`)
	}))
	t.Cleanup(srv.Close)

	var cfg okx.Config = okx.DefaultConfig()
	cfg.REST.BaseURL = srv.URL
	cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "k", "s", "p"
	cfg.REST.RequestTimeout = 2 * time.Second
	var client *okx.Client
	var err error
	client, err = okx.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	_, err = swapOf(client).Account().GetBalance(context.Background(), "BTC", "USDT")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if seen != "BTC,USDT" {
		t.Fatalf("ccy query: got %q, want %q", seen, "BTC,USDT")
	}
}

func TestContract_GetPositions(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"instId":"BTC-USDT-SWAP","posSide":"net","pos":"-0.5","avgPx":"41000",
			"upl":"-12.3","liqPx":"50000","uTime":"1629966400000"
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/account/positions": fixture,
	})
	var positions []types.PositionInfo
	var err error
	positions, err = swapOf(client).Account().GetPositions(context.Background(), "BTC-USDT-SWAP")
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("len: got %d", len(positions))
	}
	if positions[0].PosSide != types.PosSideNet {
		t.Fatalf("PosSide: got %q", positions[0].PosSide)
	}
	if !positions[0].Position.Equal(mustDec("-0.5")) {
		t.Fatalf("Position: got %v", positions[0].Position)
	}
	if !positions[0].AvgEntryPrice.Equal(mustDec("41000")) {
		t.Fatalf("AvgEntryPrice: got %v", positions[0].AvgEntryPrice)
	}
}

func TestContract_GetOpenOrders(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"instId":"BTC-USDT-SWAP","ordId":"312269865356374016","clOrdId":"my_id_1",
			"side":"buy","ordType":"limit","px":"40000","sz":"3",
			"accFillSz":"0","state":"live",
			"cTime":"1629966400000","uTime":"1629966400000"
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/orders-pending": fixture,
	})
	var orders []types.OrderInfo
	var err error
	orders, err = swapOf(client).Account().GetOpenOrders(context.Background(), "BTC-USDT-SWAP")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("len: got %d", len(orders))
	}
	if orders[0].OrderID != "312269865356374016" {
		t.Fatalf("OrderID: got %q", orders[0].OrderID)
	}
	if orders[0].ClientOrderID != "my_id_1" {
		t.Fatalf("ClientOrderID: got %q", orders[0].ClientOrderID)
	}
	if orders[0].State != types.OrderStateLive {
		t.Fatalf("State: got %q", orders[0].State)
	}
	if !orders[0].Price.Equal(mustDec("40000")) {
		t.Fatalf("Price: got %v", orders[0].Price)
	}
	if orders[0].RateLimits["ratelimit-remaining"] != "59" {
		t.Fatalf("rate-limit header missing, got %v", orders[0].RateLimits)
	}
}

func TestContract_CreateOrder_HappyPath(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"clOrdId":"abc","ordId":"312269865356374016","tag":"","sCode":"0","sMsg":""
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/order": fixture,
	})
	var info types.OrderInfo
	var err error
	info, err = swapOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:        "BTC-USDT-SWAP",
		Side:          types.SideTypeBuy,
		OrderType:     types.OrderTypeLimit,
		Price:         mustDec("40000"),
		Size:          mustDec("1"),
		ClientOrderID: "abc",
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if info.OrderID != "312269865356374016" {
		t.Fatalf("OrderID: got %q", info.OrderID)
	}
	if info.ClientOrderID != "abc" {
		t.Fatalf("ClientOrderID: got %q", info.ClientOrderID)
	}
	if info.RateLimits["ratelimit-remaining"] != "59" {
		t.Fatalf("missing rate-limit header, got %v", info.RateLimits)
	}
	// mapping должен запомниться
	var ord string
	var ok bool
	ord, ok = swapOf(client).Trading().OrderIDByClientID("abc")
	if !ok || ord != "312269865356374016" {
		t.Fatalf("clOrd→ord mapping missing")
	}
}

func TestContract_CreateOrder_RejectedByExchange(t *testing.T) {
	// Реальный ответ OKX, когда отдельный ордер отклонён биржей.
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"clOrdId":"abc","ordId":"","tag":"","sCode":"51000","sMsg":"Parameter sz error"
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/order": fixture,
	})
	var _, err = swapOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:        "BTC-USDT-SWAP",
		Side:          types.SideTypeBuy,
		OrderType:     types.OrderTypeLimit,
		Price:         mustDec("40000"),
		Size:          mustDec("1"),
		ClientOrderID: "abc",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var e *okx.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *okx.Error, got %T", err)
	}
	if e.OKXCode != "51000" {
		t.Fatalf("OKXCode: got %q", e.OKXCode)
	}
	if !strings.Contains(strings.ToLower(e.Message), "sz") {
		t.Fatalf("Message: got %q", e.Message)
	}
}

func TestContract_CreateOrder_TopLevelError(t *testing.T) {
	// Случай, когда биржа сразу отказала на уровне обёртки (auth/etc).
	var fixture string = `{"code":"50111","msg":"Invalid OK-ACCESS-KEY","data":[]}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/order": fixture,
	})
	var _, err = swapOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:    "BTC-USDT-SWAP",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeLimit,
		Price:     mustDec("40000"),
		Size:      mustDec("1"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !okx.IsAuth(err) {
		t.Fatalf("expected ErrorKindAuth, got %v", err)
	}
}

func TestContract_CancelOrder(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{"clOrdId":"abc","ordId":"312269865356374016","sCode":"0","sMsg":""}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/cancel-order": fixture,
	})
	var err error = swapOf(client).Trading().CancelOrder(context.Background(), types.CancelOrderRequest{
		InstID:  "BTC-USDT-SWAP",
		OrderID: "312269865356374016",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
}

func TestContract_NetworkTimeout(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[]}`)
	}))
	defer srv.Close()

	var cfg okx.Config = okx.DefaultConfig()
	cfg.REST.BaseURL = srv.URL
	cfg.REST.RequestTimeout = 50 * time.Millisecond
	cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "k", "s", "p"
	var client *okx.Client
	var err error
	client, err = okx.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	_, err = swapOf(client).MarketData().GetOrderBook(context.Background(), "BTC-USDT-SWAP", 5)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !okx.IsNetwork(err) {
		t.Fatalf("expected ErrorKindNetwork, got %v", err)
	}
}
