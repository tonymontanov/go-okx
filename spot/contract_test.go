/*
ФАЙЛ: spot/contract_test.go

ОПИСАНИЕ:
Contract-тесты SPOT-клиента. Покрывают парсинг реальных JSON-фикстур
OKX v5 для SPOT-эндпоинтов:
  - GetSymbolInfo           : /api/v5/public/instruments?instType=SPOT
  - GetOrderBook            : /api/v5/market/books
  - GetHistoricalCandles    : /api/v5/market/history-candles
  - GetBalance              : /api/v5/account/balance
  - GetOpenOrders           : /api/v5/trade/orders-pending?instType=SPOT
  - CreateOrder happy-path  : /api/v5/trade/order
  - CreateOrder reject      : sCode != "0" → *okx.Error
  - CreateOrder market BUY  : tgtCcy="quote_ccy" должен попадать в тело
  - CancelOrder
  - rate-limit headers пробрасываются в OrderInfo.RateLimits

Используется локальный httptest.Server.
*/

package spot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

func mustDec(s string) decimal.Decimal {
	var d decimal.Decimal
	var err error
	d, err = decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// mockOKX поднимает httptest.Server с фикстурами по path и возвращает spot-клиент.
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
	cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "k", "s", "p"
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

func spotOf(c *okx.Client) *Client { return c.Spot().(*Client) }

func TestContract_GetSymbolInfo(t *testing.T) {
	// Реальный фрагмент ответа OKX docs: GET /public/instruments?instType=SPOT
	// baseCcy/quoteCcy заполнены, CtVal/CtMult отсутствуют (только для derivatives).
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"instId":"BTC-USDT","baseCcy":"BTC","quoteCcy":"USDT",
			"tickSz":"0.1","lotSz":"0.00001","minSz":"0.00001",
			"maxLmtSz":"9999999","maxMktSz":"1000000"
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/public/instruments": fixture,
	})
	var info types.SymbolInfo
	var err error
	info, err = spotOf(client).MarketData().GetSymbolInfo(context.Background(), "BTC-USDT")
	if err != nil {
		t.Fatalf("GetSymbolInfo: %v", err)
	}
	if info.InstID != "BTC-USDT" {
		t.Fatalf("InstID: got %q", info.InstID)
	}
	if info.BaseCcy != "BTC" || info.QuoteCcy != "USDT" {
		t.Fatalf("ccy: base=%q quote=%q", info.BaseCcy, info.QuoteCcy)
	}
	if !info.TickSize.Equal(mustDec("0.1")) {
		t.Fatalf("TickSize: got %v", info.TickSize)
	}
	if !info.LotSize.Equal(mustDec("0.00001")) {
		t.Fatalf("LotSize: got %v", info.LotSize)
	}
	if info.PricePrecision != 1 {
		t.Fatalf("PricePrecision: got %d", info.PricePrecision)
	}
	if info.QuantityPrecision != 5 {
		t.Fatalf("QuantityPrecision: got %d", info.QuantityPrecision)
	}
}

func TestContract_GetSymbolInfo_NotFound(t *testing.T) {
	var fixture string = `{"code":"0","msg":"","data":[]}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/public/instruments": fixture,
	})
	var _, err = spotOf(client).MarketData().GetSymbolInfo(context.Background(), "FOO-BAR")
	if !okx.IsInvalidRequest(err) {
		t.Fatalf("expected ErrorKindInvalidRequest, got %v", err)
	}
}

func TestContract_GetOrderBook(t *testing.T) {
	// Формат /market/books идентичен для SPOT и SWAP.
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
	ob, err = spotOf(client).MarketData().GetOrderBook(context.Background(), "BTC-USDT", 20)
	if err != nil {
		t.Fatalf("GetOrderBook: %v", err)
	}
	if len(ob.Asks) != 2 || len(ob.Bids) != 2 {
		t.Fatalf("levels count: asks=%d bids=%d", len(ob.Asks), len(ob.Bids))
	}
	if !ob.Asks[0].Price.Equal(mustDec("41006.8")) {
		t.Fatalf("ask[0].Price: %v", ob.Asks[0].Price)
	}
	if ob.Ts != 1629966436396 {
		t.Fatalf("Ts: %d", ob.Ts)
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
	candles, err = spotOf(client).MarketData().GetHistoricalCandles(context.Background(), "BTC-USDT", types.Timeframe1m, 2)
	if err != nil {
		t.Fatalf("GetHistoricalCandles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len: %d", len(candles))
	}
	if !candles[0].Open.Equal(mustDec("3.721")) {
		t.Fatalf("Open[0]: %v", candles[0].Open)
	}
	if !candles[0].Closed {
		t.Fatal("Closed[0] must be true")
	}
}

func TestContract_GetHistoricalCandles_SubMinute(t *testing.T) {
	var _, client = mockOKX(t, map[string]string{})
	var _, err = spotOf(client).MarketData().GetHistoricalCandles(context.Background(), "BTC-USDT", types.Timeframe1s, 10)
	if !okx.IsInvalidRequest(err) {
		t.Fatalf("expected InvalidRequest, got %v", err)
	}
}

func TestContract_GetBalance(t *testing.T) {
	// Unified-account /account/balance общий для SPOT и SWAP.
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{
			"uTime":"1614847029331",
			"totalEq":"22000",
			"isoEq":"0","adjEq":"22000",
			"ordFroz":"0","imr":"0","mmr":"0","mgnRatio":"99999",
			"notionalUsd":"0",
			"details":[
				{"ccy":"BTC","eq":"0.5","cashBal":"0.5","isoEq":"0","availEq":"0.5",
				 "disEq":"22000","availBal":"0.5","frozenBal":"0","ordFrozen":"0",
				 "upl":"0","isoUpl":"0","mgnRatio":"99999","eqUsd":"22000",
				 "uTime":"1614847029331"},
				{"ccy":"USDT","eq":"10","cashBal":"10","isoEq":"0","availEq":"10",
				 "disEq":"10","availBal":"10","frozenBal":"0","ordFrozen":"0",
				 "upl":"0","isoUpl":"0","mgnRatio":"99999","eqUsd":"10",
				 "uTime":"1614847029331"}
			]
		}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/account/balance": fixture,
	})
	var bal types.Balance
	var err error
	bal, err = spotOf(client).Account().GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if !bal.TotalEquityUSD.Equal(mustDec("22000")) {
		t.Fatalf("TotalEquityUSD: %v", bal.TotalEquityUSD)
	}
	if len(bal.Details) != 2 {
		t.Fatalf("details: %d", len(bal.Details))
	}

	var btc *types.BalanceDetail
	var i int
	for i = 0; i < len(bal.Details); i++ {
		if bal.Details[i].Ccy == "BTC" {
			btc = &bal.Details[i]
			break
		}
	}
	if btc == nil {
		t.Fatal("BTC detail missing")
	}
	if !btc.AvailableBalance.Equal(mustDec("0.5")) {
		t.Fatalf("BTC.AvailableBalance: %v", btc.AvailableBalance)
	}
	if !btc.CashBalance.Equal(mustDec("0.5")) {
		t.Fatalf("BTC.CashBalance: %v", btc.CashBalance)
	}
}

func TestContract_GetBalance_FilteredCcy(t *testing.T) {
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

	_, err = spotOf(client).Account().GetBalance(context.Background(), "BTC", "USDT")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if seen != "BTC,USDT" {
		t.Fatalf("ccy query: got %q, want BTC,USDT", seen)
	}
}

func TestContract_GetOpenOrders(t *testing.T) {
	// instType=SPOT должен быть передан в query.
	var seenInstType string
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInstType = r.URL.Query().Get("instType")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"code":"0","msg":"",
			"data":[{
				"instId":"BTC-USDT","ordId":"312269865356374016","clOrdId":"myId1",
				"side":"buy","ordType":"limit","px":"40000","sz":"0.1",
				"accFillSz":"0","state":"live",
				"cTime":"1629966400000","uTime":"1629966400000"
			}]
		}`)
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

	var orders []types.OrderInfo
	orders, err = spotOf(client).Account().GetOpenOrders(context.Background(), "BTC-USDT")
	if err != nil {
		t.Fatalf("GetOpenOrders: %v", err)
	}
	if seenInstType != "SPOT" {
		t.Fatalf("instType query: got %q, want SPOT", seenInstType)
	}
	if len(orders) != 1 {
		t.Fatalf("len: %d", len(orders))
	}
	if orders[0].ClientOrderID != "myId1" {
		t.Fatalf("ClientOrderID: %q", orders[0].ClientOrderID)
	}
	if !orders[0].Price.Equal(mustDec("40000")) {
		t.Fatalf("Price: %v", orders[0].Price)
	}
}

func TestContract_CreateOrder_HappyPath(t *testing.T) {
	// tdMode на спот должен по умолчанию проставиться как "cash".
	var seenBody map[string]any
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ratelimit-remaining", "42")
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[{"clOrdId":"abc","ordId":"1234","sCode":"0","sMsg":""}]}`)
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

	var info types.OrderInfo
	info, err = spotOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:        "BTC-USDT",
		Side:          types.SideTypeBuy,
		OrderType:     types.OrderTypeLimit,
		Price:         mustDec("40000"),
		Size:          mustDec("0.1"),
		ClientOrderID: "abc",
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if info.OrderID != "1234" {
		t.Fatalf("OrderID: %q", info.OrderID)
	}
	if info.RateLimits["ratelimit-remaining"] != "42" {
		t.Fatalf("rate-limit header missing, got %v", info.RateLimits)
	}
	// Проверяем что tdMode = cash (default для spot), posSide НЕТ.
	if got, _ := seenBody["tdMode"].(string); got != "cash" {
		t.Fatalf("tdMode: got %q, want cash", got)
	}
	if _, has := seenBody["posSide"]; has {
		t.Fatal("posSide must NOT be set for spot")
	}
	if _, has := seenBody["reduceOnly"]; has {
		t.Fatal("reduceOnly must NOT be set for spot")
	}

	// mapping
	var ord string
	var ok bool
	ord, ok = spotOf(client).Trading().OrderIDByClientID("abc")
	if !ok || ord != "1234" {
		t.Fatal("clOrd→ord mapping missing")
	}
}

func TestContract_CreateOrder_MarketBuy_WithTgtCcy(t *testing.T) {
	// Для market BUY с tgtCcy=quote_ccy, sz интерпретируется как USDT.
	var seenBody map[string]any
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[{"clOrdId":"q","ordId":"99","sCode":"0"}]}`)
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

	_, err = spotOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:    "BTC-USDT",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeMarket,
		Size:      mustDec("100"),
		TgtCcy:    types.TgtCcyQuote,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if got, _ := seenBody["tgtCcy"].(string); got != "quote_ccy" {
		t.Fatalf("tgtCcy: got %q, want quote_ccy", got)
	}
	if got, _ := seenBody["ordType"].(string); got != "market" {
		t.Fatalf("ordType: got %q, want market", got)
	}
	// market — не должно быть px
	if _, has := seenBody["px"]; has {
		t.Fatal("px must NOT be set for market order")
	}
}

func TestContract_CreateOrder_MarketBuy_NoTgtCcy_NotSent(t *testing.T) {
	// Если TgtCcy пуст, поле не должно отправляться (OKX default = quote_ccy для buy).
	var seenBody map[string]any
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[{"clOrdId":"","ordId":"1","sCode":"0"}]}`)
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

	_, err = spotOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:    "BTC-USDT",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeMarket,
		Size:      mustDec("100"),
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if _, has := seenBody["tgtCcy"]; has {
		t.Fatal("tgtCcy must NOT be sent when not specified")
	}
}

func TestContract_CreateOrder_LimitIgnoresTgtCcy(t *testing.T) {
	// Для limit-ордера tgtCcy игнорируется (OKX это не поле для limit).
	var seenBody map[string]any
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[{"clOrdId":"","ordId":"1","sCode":"0"}]}`)
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

	_, err = spotOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:    "BTC-USDT",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeLimit,
		Price:     mustDec("40000"),
		Size:      mustDec("0.1"),
		TgtCcy:    types.TgtCcyQuote,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if _, has := seenBody["tgtCcy"]; has {
		t.Fatal("tgtCcy must NOT be sent for limit order")
	}
}

func TestContract_CreateOrder_RejectedByExchange(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{"clOrdId":"abc","ordId":"","tag":"","sCode":"51000","sMsg":"Parameter sz error"}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/order": fixture,
	})
	var _, err = spotOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:    "BTC-USDT",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeLimit,
		Price:     mustDec("40000"),
		Size:      mustDec("0.1"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var e *okx.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *okx.Error, got %T", err)
	}
	if e.OKXCode != "51000" {
		t.Fatalf("OKXCode: %q", e.OKXCode)
	}
	if !strings.Contains(strings.ToLower(e.Message), "sz") {
		t.Fatalf("Message: %q", e.Message)
	}
}

func TestContract_CreateOrder_TopLevelAuthError(t *testing.T) {
	var fixture string = `{"code":"50111","msg":"Invalid OK-ACCESS-KEY","data":[]}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/order": fixture,
	})
	var _, err = spotOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:    "BTC-USDT",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeLimit,
		Price:     mustDec("40000"),
		Size:      mustDec("0.1"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !okx.IsAuth(err) {
		t.Fatalf("expected ErrorKindAuth, got %v", err)
	}
}

func TestContract_CreateOrder_InvalidClientOrderID(t *testing.T) {
	// Подчёркивания запрещены в clOrdId OKX.
	var _, client = mockOKX(t, map[string]string{})
	var _, err = spotOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
		InstID:        "BTC-USDT",
		Side:          types.SideTypeBuy,
		OrderType:     types.OrderTypeLimit,
		Price:         mustDec("40000"),
		Size:          mustDec("0.1"),
		ClientOrderID: "with_underscore",
	})
	if !okx.IsInvalidRequest(err) {
		t.Fatalf("expected InvalidRequest, got %v", err)
	}
}

func TestContract_CancelOrder(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{"clOrdId":"","ordId":"312269865356374016","sCode":"0","sMsg":""}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/cancel-order": fixture,
	})
	var err error = spotOf(client).Trading().CancelOrder(context.Background(), types.CancelOrderRequest{
		InstID:  "BTC-USDT",
		OrderID: "312269865356374016",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
}

func TestContract_ModifyOrder(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{"clOrdId":"","ordId":"1","sCode":"0","sMsg":""}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/amend-order": fixture,
	})
	var info types.OrderInfo
	var err error
	info, err = spotOf(client).Trading().ModifyOrder(context.Background(), types.ModifyOrderRequest{
		InstID:   "BTC-USDT",
		OrderID:  "1",
		NewPrice: mustDec("41000"),
	})
	if err != nil {
		t.Fatalf("ModifyOrder: %v", err)
	}
	if info.OrderID != "1" {
		t.Fatalf("OrderID: %q", info.OrderID)
	}
	if !info.Price.Equal(mustDec("41000")) {
		t.Fatalf("Price: %v", info.Price)
	}
}

func TestContract_CancelAllAfter(t *testing.T) {
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{"triggerTime":"1700000010000","ts":"1700000000000"}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/cancel-all-after": fixture,
	})
	var res types.CancelAllAfterResult
	var err error
	res, err = spotOf(client).Trading().CancelAllAfter(context.Background(), 30*time.Second)
	if err != nil {
		t.Fatalf("CancelAllAfter: %v", err)
	}
	if res.TriggerTimeMs != 1700000010000 {
		t.Fatalf("TriggerTimeMs: %d", res.TriggerTimeMs)
	}
	if res.TsMs != 1700000000000 {
		t.Fatalf("TsMs: %d", res.TsMs)
	}
}

func TestContract_CancelAllAfter_Disarm(t *testing.T) {
	// При timeout=0 OKX возвращает triggerTime=""; парсинг должен дать 0.
	var fixture string = `{
		"code":"0","msg":"",
		"data":[{"triggerTime":"","ts":"1700000000000"}]
	}`
	var _, client = mockOKX(t, map[string]string{
		"/api/v5/trade/cancel-all-after": fixture,
	})
	var res types.CancelAllAfterResult
	var err error
	res, err = spotOf(client).Trading().CancelAllAfter(context.Background(), 0)
	if err != nil {
		t.Fatalf("CancelAllAfter: %v", err)
	}
	if res.TriggerTimeMs != 0 {
		t.Fatalf("TriggerTimeMs must be 0 on disarm, got %d", res.TriggerTimeMs)
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

	_, err = spotOf(client).MarketData().GetOrderBook(context.Background(), "BTC-USDT", 5)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !okx.IsNetwork(err) {
		t.Fatalf("expected ErrorKindNetwork, got %v", err)
	}
}
