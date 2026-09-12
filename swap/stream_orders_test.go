/*
FILE: swap/stream_orders_test.go

DESCRIPTION:
Tests for the private "orders" channel (SWAP):
  1. parseOrderPush on a push composed after the OKX docs example (WS Order
     channel): the per-update fill fields (fillSz in contracts, fillPx, tradeId,
     execType, fillTime, fillFee, fillFeeCcy, fillPnl) and amendResult/
     cancelSource are exposed on types.OrderInfo; an amend push carries no fill.
  2. WatchOpenOrdersWithReset against a mock private server: onReset fires once
     per connection BEFORE that connection's pushes, and again after the server
     drops the socket and the client reconnects.
*/

package swap

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
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// swapOrderPushPartial — partial fill of a SWAP limit buy: 2 of 10 contracts
// filled at 30000.5 as taker (fields after the OKX docs WS Order channel example).
const swapOrderPushPartial string = `{"accFillSz":"2","amendResult":"","avgPx":"30000.5","cancelSource":"","category":"normal","clOrdId":"abc123","cTime":"1700000000000","execType":"T","fee":"-0.3","feeCcy":"USDT","fillFee":"-0.3","fillFeeCcy":"USDT","fillNotionalUsd":"600.01","fillPx":"30000.5","fillSz":"2","fillPnl":"1.5","fillTime":"1700000001000","instId":"BTC-USDT-SWAP","instType":"SWAP","lever":"10","ordId":"777","ordType":"limit","pnl":"0","posSide":"net","px":"30000.5","reduceOnly":"false","reqId":"","side":"buy","state":"partially_filled","sz":"10","tdMode":"cross","tradeId":"555","uTime":"1700000001001"}`

// swapOrderPushAmended — successful amendment (amendResult "0") of the same
// order: no fill in this update.
const swapOrderPushAmended string = `{"accFillSz":"2","amendResult":"0","avgPx":"30000.5","cancelSource":"","clOrdId":"abc123","cTime":"1700000000000","execType":"","fillFee":"0","fillFeeCcy":"","fillPx":"","fillSz":"0","fillPnl":"0","fillTime":"","instId":"BTC-USDT-SWAP","instType":"SWAP","ordId":"777","ordType":"limit","px":"29990","reqId":"r1","side":"buy","state":"partially_filled","sz":"12","tradeId":"","uTime":"1700000002000"}`

func swapDec(s string) decimal.Decimal {
	var d decimal.Decimal
	d, _ = decimal.NewFromString(s)
	return d
}

func TestParseOrderPush_SwapFillFields(t *testing.T) {
	var pushes []rawOrderPush
	if err := codec.Unmarshal([]byte("["+swapOrderPushPartial+"]"), &pushes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var info types.OrderInfo = parseOrderPush(pushes[0])
	if info.OrderID != "777" || info.ClientOrderID != "abc123" || info.InstID != "BTC-USDT-SWAP" || info.Side != types.SideTypeBuy {
		t.Fatalf("identity: %+v", info)
	}
	if info.State != types.OrderStatePartiallyFilled || !info.Size.Equal(swapDec("10")) || !info.FilledSize.Equal(swapDec("2")) {
		t.Fatalf("state/size: %+v", info)
	}
	if !info.FillSize.Equal(swapDec("2")) || !info.FillPrice.Equal(swapDec("30000.5")) {
		t.Fatalf("fillSz/fillPx: %s %s", info.FillSize, info.FillPrice)
	}
	if info.TradeID != "555" || info.ExecType != "T" || info.FillTimeMs != 1700000001000 {
		t.Fatalf("tradeId/execType/fillTime: %q %q %d", info.TradeID, info.ExecType, info.FillTimeMs)
	}
	if !info.FillFee.Equal(swapDec("-0.3")) || info.FillFeeCcy != "USDT" || !info.FillPnl.Equal(swapDec("1.5")) {
		t.Fatalf("fillFee/fillFeeCcy/fillPnl: %s %q %s", info.FillFee, info.FillFeeCcy, info.FillPnl)
	}
	if info.AmendResult != "" || info.CancelSource != "" {
		t.Fatalf("amendResult/cancelSource must be empty on a fill push: %q %q", info.AmendResult, info.CancelSource)
	}
}

func TestParseOrderPush_SwapAmendCarriesNoFill(t *testing.T) {
	var pushes []rawOrderPush
	if err := codec.Unmarshal([]byte("["+swapOrderPushAmended+"]"), &pushes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var info types.OrderInfo = parseOrderPush(pushes[0])
	if info.AmendResult != "0" || info.State != types.OrderStatePartiallyFilled {
		t.Fatalf("amend push: %+v", info)
	}
	if !info.FillSize.IsZero() || !info.FillPrice.IsZero() || info.TradeID != "" || info.FillTimeMs != 0 {
		t.Fatalf("amend push must carry no fill: %+v", info)
	}
	if !info.Price.Equal(swapDec("29990")) || !info.Size.Equal(swapDec("12")) || !info.FilledSize.Equal(swapDec("2")) {
		t.Fatalf("px/sz/accFillSz after amend: %s %s %s", info.Price, info.Size, info.FilledSize)
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
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"event":"subscribe","arg":{"channel":"orders","instType":"SWAP","instId":"BTC-USDT-SWAP"}}`))
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

func TestStream_WatchOpenOrdersWithReset_ResetPerConnection(t *testing.T) {
	var push string = `{"arg":{"channel":"orders","instType":"SWAP","instId":"BTC-USDT-SWAP"},"data":[` + swapOrderPushPartial + `]}`
	var url string
	var srv *httptest.Server
	var conns *atomic.Int64
	url, srv, conns = startOrdersMockWS(t, push, true)
	defer srv.Close()

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
	defer oc.Close()
	var sc *Client = NewClient(oc)

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
	err = sc.Stream().WatchOpenOrdersWithReset(ctx, "BTC-USDT-SWAP", func(orders []types.OrderInfo) {
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
	if lp.TradeID != "555" || !lp.FillSize.Equal(swapDec("2")) || lp.ExecType != "T" {
		t.Fatalf("push payload lost fill fields: %+v", lp)
	}
}
