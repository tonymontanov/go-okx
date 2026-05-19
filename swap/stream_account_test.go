/*
FILE: swap/stream_account_test.go

DESCRIPTION:
Integration test for WatchAccount: starts a local mock WS server that
simulates the OKX private endpoint (login + account channel push), and verifies:
  - the SDK successfully performs login and subscribe;
  - the incoming account push is parsed into types.Balance with correct numbers;
  - the callback is called transparently (without knowledge of reconnect/login).
*/

package swap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

func TestStream_WatchAccount(t *testing.T) {
	const accountPush string = `{
		"arg":{"channel":"account"},
		"data":[{
			"uTime":"1614847029331",
			"totalEq":"91884","adjEq":"91884","isoEq":"0",
			"ordFroz":"0","imr":"0","mmr":"0",
			"mgnRatio":"99999","notionalUsd":"0",
			"details":[{
				"ccy":"USDT","eq":"91884","cashBal":"91884","isoEq":"0",
				"availEq":"91884","disEq":"91884","availBal":"91884",
				"frozenBal":"0","ordFrozen":"0","upl":"0","isoUpl":"0",
				"mgnRatio":"99999","eqUsd":"91884","uTime":"1614847029331"
			}]
		}]
	}`

	var upgrader websocket.Upgrader = websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var conn *websocket.Conn
		var err error
		conn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			var msgType int
			var raw []byte
			msgType, raw, err = conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType != websocket.TextMessage {
				continue
			}
			var s string = string(raw)
			switch {
			case s == "ping":
				_ = conn.WriteMessage(websocket.TextMessage, []byte("pong"))
			case strings.Contains(s, `"op":"login"`):
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"login","code":"0","msg":""}`))
			case strings.Contains(s, `"op":"subscribe"`) && strings.Contains(s, `"channel":"account"`):
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"subscribe","arg":{"channel":"account"}}`))
				_ = conn.WriteMessage(websocket.TextMessage, []byte(accountPush))
			}
		}
	}))
	defer srv.Close()

	var wsURL string = strings.Replace(srv.URL, "http://", "ws://", 1)

	var cfg okx.Config = okx.DefaultConfig()
	cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "k", "s", "p"
	cfg.WS.PrivateURL = wsURL
	cfg.WS.HandshakeTimeout = 2 * time.Second
	cfg.WS.PingInterval = 200 * time.Millisecond

	var client *okx.Client
	var err error
	client, err = okx.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	var got atomic.Int64
	var last types.Balance
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	err = swapOf(client).Stream().WatchAccount(ctx, func(b types.Balance) {
		last = b
		got.Add(1)
	}, func(e error) {
		t.Logf("err: %v", e)
	})
	if err != nil {
		t.Fatalf("WatchAccount: %v", err)
	}

	var deadline time.Time = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got.Load() == 0 {
		t.Fatal("WatchAccount: callback was never invoked")
	}

	if !last.TotalEquityUSD.Equal(mustDec("91884")) {
		t.Fatalf("TotalEquityUSD: got %v, want 91884", last.TotalEquityUSD)
	}
	if !last.MarginRatio.Equal(mustDec("99999")) {
		t.Fatalf("MarginRatio: got %v, want 99999", last.MarginRatio)
	}
	if last.UpdatedAtMs != 1614847029331 {
		t.Fatalf("UpdatedAtMs: got %d", last.UpdatedAtMs)
	}
	if len(last.Details) != 1 || last.Details[0].Ccy != "USDT" {
		t.Fatalf("details: got %#v", last.Details)
	}
	if !last.Details[0].AvailableEquity.Equal(mustDec("91884")) {
		t.Fatalf("USDT.AvailableEquity: got %v", last.Details[0].AvailableEquity)
	}
}

func TestStream_WatchAccount_RequiresCredentials(t *testing.T) {
	var cfg okx.Config = okx.DefaultConfig()
	var client *okx.Client
	var err error
	client, err = okx.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	err = swapOf(client).Stream().WatchAccount(context.Background(), func(types.Balance) {}, nil)
	if !okx.IsAuth(err) {
		t.Fatalf("expected ErrorKindAuth, got %v", err)
	}
}
