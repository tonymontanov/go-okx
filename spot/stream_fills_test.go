/*
FILE: spot/stream_fills_test.go

DESCRIPTION:
Smoke test for the private "fills" channel. The subscription goes through
privateConn, so the mock server must correctly handle login (without signature
verification — that is handled at the internal/ws level) and then push a single fill.
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
	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

// startPrivateSubMockWS — starts a mock server that handles login + ack
// on subscribe + one push (with channel/instId/instType substitution).
// seenChannel stores the name of the last subscribe channel.
func startPrivateSubMockWS(t *testing.T, push string) (string, *httptest.Server, *atomic.Pointer[string]) {
	t.Helper()
	var upgrader websocket.Upgrader = websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	var seen *atomic.Pointer[string] = &atomic.Pointer[string]{}
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c *websocket.Conn
		var err error
		c, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
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
			// subscribe?
			var sp struct {
				Op   string `json:"op"`
				Args []struct {
					Channel  string `json:"channel"`
					InstID   string `json:"instId"`
					InstType string `json:"instType"`
				} `json:"args"`
			}
			if err = codec.Unmarshal(message, &sp); err == nil && sp.Op == "subscribe" && len(sp.Args) > 0 {
				var arg = sp.Args[0]
				seen.Store(&arg.Channel)
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"event":"subscribe","arg":{"channel":"`+arg.Channel+`","instType":"`+arg.InstType+`","instId":"`+arg.InstID+`"}}`))
				var enriched string = strings.ReplaceAll(push, "{CHANNEL}", arg.Channel)
				enriched = strings.ReplaceAll(enriched, "{INSTID}", arg.InstID)
				enriched = strings.ReplaceAll(enriched, "{INSTTYPE}", arg.InstType)
				_ = c.WriteMessage(websocket.TextMessage, []byte(enriched))
			}
		}
	}))
	var u string = strings.Replace(srv.URL, "http://", "ws://", 1)
	return u, srv, seen
}

func TestStream_WatchFills_SubscribesToFillsAndParses(t *testing.T) {
	// One fill with key fields populated.
	var push string = `{"arg":{"channel":"{CHANNEL}","instType":"{INSTTYPE}"},"data":[{
		"instType":"SPOT","instId":"BTC-USDT","tradeId":"t-1","ordId":"o-1",
		"clOrdId":"cli-1","billId":"b-1","tag":"","fillPx":"60000","fillSz":"0.5",
		"side":"buy","posSide":"","execType":"T","feeCcy":"USDT","fee":"-0.06",
		"fillPnl":"12.3","fillTime":"1700000000000","ts":"1700000000050"
	}]}`
	var url string
	var srv *httptest.Server
	var seen *atomic.Pointer[string]
	url, srv, seen = startPrivateSubMockWS(t, push)
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

	var sc *Client = oc.Spot().(*Client)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var gotMu sync.Mutex
	var got types.Fill
	var gotCh chan struct{} = make(chan struct{}, 1)
	err = sc.Stream().WatchFills(ctx, "", func(f types.Fill) {
		gotMu.Lock()
		got = f
		gotMu.Unlock()
		select {
		case gotCh <- struct{}{}:
		default:
		}
	}, nil)
	if err != nil {
		t.Fatalf("WatchFills: %v", err)
	}

	select {
	case <-gotCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("no fill received")
	}

	if ch := seen.Load(); ch == nil || *ch != "fills" {
		var got string
		if ch != nil {
			got = *ch
		}
		t.Fatalf("expected subscribe to fills, got %q", got)
	}

	gotMu.Lock()
	defer gotMu.Unlock()
	if got.OrdID != "o-1" || got.ClOrdID != "cli-1" {
		t.Fatalf("ids mismatch: %+v", got)
	}
	if !got.FillPx.Equal(mustDec("60000")) || !got.FillSz.Equal(mustDec("0.5")) {
		t.Fatalf("px/sz mismatch: %+v", got)
	}
	if !got.Fee.Equal(mustDec("-0.06")) {
		t.Fatalf("fee mismatch: %v", got.Fee)
	}
	if got.ExecType != "T" {
		t.Fatalf("execType mismatch: %q", got.ExecType)
	}
	if got.FillTime != 1700000000000 {
		t.Fatalf("fillTime mismatch: %d", got.FillTime)
	}
}
