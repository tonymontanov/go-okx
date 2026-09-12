/*
FILE: spot/stream_orders_test.go

DESCRIPTION:
Tests for the private "orders" channel:
  1. parseOrderPush on the push example from the OKX docs (WS Order channel):
     the per-update fill fields (fillSz/fillPx/tradeId/execType/fillTime/
     fillFee/fillFeeCcy/fillPnl) and amendResult/cancelSource are exposed on
     types.OrderInfo; a cancel push carries no fill.
  2. WatchOpenOrdersWithReset against a mock private server: onReset fires once
     per connection BEFORE that connection's pushes, and again after the server
     drops the socket and the client reconnects.
*/

package spot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

func dec(s string) decimal.Decimal {
	var d decimal.Decimal
	d, _ = decimal.NewFromString(s)
	return d
}

// docsOrderPushFilled — push example from the OKX docs (WS Order channel):
// SPOT limit sell 0.001 BTC-USDT fully filled at 31527.1 as maker.
const docsOrderPushFilled string = `{"accFillSz":"0.001","algoClOrdId":"","algoId":"","amendResult":"","amendSource":"","avgPx":"31527.1","cancelSource":"","category":"normal","ccy":"","clOrdId":"","code":"0","cTime":"1654084334977","execType":"M","fee":"-0.02522168","feeCcy":"USDT","fillFee":"-0.02522168","fillFeeCcy":"USDT","fillNotionalUsd":"31.50818374","fillPx":"31527.1","fillSz":"0.001","fillPnl":"0.01","fillTime":"1654084353263","fillPxVol":"","fillPxUsd":"","fillMarkVol":"","fillFwdPx":"","fillMarkPx":"","fillIdxPx":"","instId":"BTC-USDT","instType":"SPOT","lever":"0","msg":"","notionalUsd":"31.50818374","ordId":"452197707845865472","ordType":"limit","pnl":"0","posSide":"","px":"31527.1","rebate":"0","rebateCcy":"BTC","reduceOnly":"false","reqId":"","side":"sell","source":"","state":"filled","sz":"0.001","tag":"","tdMode":"cash","tgtCcy":"","tradeId":"242589207","tradeQuoteCcy":"USDT","uTime":"1654084353264"}`

// docsOrderPushCanceled — a cancel-by-user push (cancelSource "1"): no fill in
// this update — fillSz "0", fillPx "", tradeId "".
const docsOrderPushCanceled string = `{"accFillSz":"0","amendResult":"","avgPx":"","cancelSource":"1","clOrdId":"cli1","cTime":"1654084334977","execType":"","fillFee":"0","fillFeeCcy":"","fillPx":"","fillSz":"0","fillPnl":"0","fillTime":"","instId":"BTC-USDT","instType":"SPOT","ordId":"1","ordType":"post_only","px":"30000","side":"buy","state":"canceled","sz":"0.5","tradeId":"","uTime":"1654084353300"}`

func TestParseOrderPush_FillFieldsFromDocsExample(t *testing.T) {
	var pushes []rawOrderPush
	if err := codec.Unmarshal([]byte("["+docsOrderPushFilled+"]"), &pushes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var info types.OrderInfo = parseOrderPush(pushes[0])
	if info.OrderID != "452197707845865472" || info.InstID != "BTC-USDT" || info.Side != types.SideTypeSell {
		t.Fatalf("identity: %+v", info)
	}
	if info.State != types.OrderStateFilled || !info.FilledSize.Equal(dec("0.001")) || !info.Size.Equal(dec("0.001")) {
		t.Fatalf("state/size: %+v", info)
	}
	if !info.FillSize.Equal(dec("0.001")) || !info.FillPrice.Equal(dec("31527.1")) {
		t.Fatalf("fillSz/fillPx: %s %s", info.FillSize, info.FillPrice)
	}
	if info.TradeID != "242589207" || info.ExecType != "M" || info.FillTimeMs != 1654084353263 {
		t.Fatalf("tradeId/execType/fillTime: %q %q %d", info.TradeID, info.ExecType, info.FillTimeMs)
	}
	if !info.FillFee.Equal(dec("-0.02522168")) || info.FillFeeCcy != "USDT" || !info.FillPnl.Equal(dec("0.01")) {
		t.Fatalf("fillFee/fillFeeCcy/fillPnl: %s %q %s", info.FillFee, info.FillFeeCcy, info.FillPnl)
	}
	if info.AmendResult != "" || info.CancelSource != "" {
		t.Fatalf("amendResult/cancelSource must be empty on a fill push: %q %q", info.AmendResult, info.CancelSource)
	}
	if info.CreatedAtMs != 1654084334977 || info.UpdatedAtMs != 1654084353264 {
		t.Fatalf("cTime/uTime: %d %d", info.CreatedAtMs, info.UpdatedAtMs)
	}
}

func TestParseOrderPush_CancelCarriesNoFill(t *testing.T) {
	var pushes []rawOrderPush
	if err := codec.Unmarshal([]byte("["+docsOrderPushCanceled+"]"), &pushes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var info types.OrderInfo = parseOrderPush(pushes[0])
	if info.State != types.OrderStateCanceled || info.CancelSource != "1" || info.ClientOrderID != "cli1" {
		t.Fatalf("cancel push: %+v", info)
	}
	if !info.FillSize.IsZero() || !info.FillPrice.IsZero() || info.TradeID != "" || info.FillTimeMs != 0 {
		t.Fatalf("cancel push must carry no fill: %+v", info)
	}
	if !info.Price.Equal(dec("30000")) || !info.Size.Equal(dec("0.5")) || !info.FilledSize.IsZero() {
		t.Fatalf("px/sz/accFillSz: %s %s %s", info.Price, info.Size, info.FilledSize)
	}
}

// startOrdersMockWS — private mock server: login ack, subscribe ack followed by
// one orders push per subscribe; when dropFirst is set, the FIRST connection is
// closed right after its push so the client reconnects. conns counts accepted
// connections.
func startOrdersMockWS(t *testing.T, push string, dropFirst bool) (string, *httptest.Server, *atomic.Int64) {
	t.Helper()
	var upgrader websocket.Upgrader = websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	var conns *atomic.Int64 = &atomic.Int64{}
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c *websocket.Conn
		var err error
		c, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var connNo int64 = conns.Add(1)
		for {
			var msgType int
			var message []byte
			msgType, message, err = c.ReadMessage()
			if err != nil {
				return
			}
			if msgType != websocket.TextMessage {
				continue
			}
			var s string = string(message)
			if s == "ping" {
				_ = c.WriteMessage(websocket.TextMessage, []byte("pong"))
				continue
			}
			if strings.Contains(s, `"op":"login"`) {
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"event":"login","code":"0","msg":""}`))
				continue
			}
			if strings.Contains(s, `"op":"subscribe"`) && strings.Contains(s, `"channel":"orders"`) {
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"event":"subscribe","arg":{"channel":"orders","instType":"SPOT","instId":"BTC-USDT"}}`))
				_ = c.WriteMessage(websocket.TextMessage, []byte(push))
				if dropFirst && connNo == 1 {
					return // closes the socket → client reconnects
				}
			}
		}
	}))
	var u string = strings.Replace(srv.URL, "http://", "ws://", 1)
	return u, srv, conns
}

func newMockClient(t *testing.T, url string) *okx.Client {
	t.Helper()
	var cfg okx.Config = okx.DefaultConfig()
	cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "k", "s", "p"
	cfg.WS.PublicURL = url
	cfg.WS.PrivateURL = url
	cfg.WS.HandshakeTimeout = 2 * time.Second
	cfg.WS.ReadTimeout = 2 * time.Second
	cfg.WS.WriteTimeout = 1 * time.Second
	cfg.WS.PingInterval = 200 * time.Millisecond
	cfg.WS.ReconnectInitialBackoff = 20 * time.Millisecond
	cfg.WS.ReconnectMaxBackoff = 100 * time.Millisecond
	var oc *okx.Client
	var err error
	oc, err = okx.NewClient(cfg)
	if err != nil {
		t.Fatalf("okx.NewClient: %v", err)
	}
	return oc
}

func TestStream_WatchOpenOrdersWithReset_ResetPerConnection(t *testing.T) {
	var push string = `{"arg":{"channel":"orders","instType":"SPOT","instId":"BTC-USDT"},"data":[` + docsOrderPushFilled + `]}`
	var url string
	var srv *httptest.Server
	var conns *atomic.Int64
	url, srv, conns = startOrdersMockWS(t, push, true)
	defer srv.Close()

	var oc *okx.Client = newMockClient(t, url)
	defer oc.Close()
	var sc *Client = oc.Spot().(*Client)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	var seq []string
	var record = func(s string) {
		mu.Lock()
		seq = append(seq, s)
		mu.Unlock()
	}
	var snapshot = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seq...)
	}
	var lastPush types.OrderInfo
	var err error = sc.Stream().WatchOpenOrdersWithReset(ctx, "BTC-USDT", func(orders []types.OrderInfo) {
		mu.Lock()
		lastPush = orders[0]
		mu.Unlock()
		record("push")
	}, func() { record("reset") }, nil)
	if err != nil {
		t.Fatalf("WatchOpenOrdersWithReset: %v", err)
	}

	var deadline time.Time = time.Now().Add(4 * time.Second)
	for len(snapshot()) < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	var got []string = snapshot()
	if len(got) < 4 {
		t.Fatalf("sequence = %v, want at least [reset push reset push]", got)
	}
	if got[0] != "reset" || got[1] != "push" || got[2] != "reset" || got[3] != "push" {
		t.Fatalf("sequence = %v, want [reset push reset push ...]", got)
	}
	if conns.Load() < 2 {
		t.Fatalf("connections = %d, want a reconnect", conns.Load())
	}
	mu.Lock()
	var lp types.OrderInfo = lastPush
	mu.Unlock()
	if lp.TradeID != "242589207" || !lp.FillSize.Equal(dec("0.001")) || lp.ExecType != "M" {
		t.Fatalf("push payload lost fill fields: %+v", lp)
	}
}

func TestStream_WatchOpenOrdersWithReset_NilResetDegradesToPlain(t *testing.T) {
	var push string = `{"arg":{"channel":"orders","instType":"SPOT","instId":"BTC-USDT"},"data":[` + docsOrderPushCanceled + `]}`
	var url string
	var srv *httptest.Server
	url, srv, _ = startOrdersMockWS(t, push, false)
	defer srv.Close()

	var oc *okx.Client = newMockClient(t, url)
	defer oc.Close()
	var sc *Client = oc.Spot().(*Client)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var gotCh chan types.OrderInfo = make(chan types.OrderInfo, 4)
	var err error = sc.Stream().WatchOpenOrdersWithReset(ctx, "BTC-USDT", func(orders []types.OrderInfo) {
		gotCh <- orders[0]
	}, nil, nil)
	if err != nil {
		t.Fatalf("WatchOpenOrdersWithReset(nil reset): %v", err)
	}
	select {
	case got := <-gotCh:
		if got.State != types.OrderStateCanceled || got.CancelSource != "1" {
			t.Fatalf("push = %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no push received")
	}
}
