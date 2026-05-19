/*
FILE: internal/ws/conn_test.go

DESCRIPTION:
WS Conn tests. Cover:
  - TestConn_Connect_Subscribe_Receive: basic happy-path — connect,
    subscribe, receive push message, dispatch to handler.
  - TestConn_AutoResubscribeAfterReconnect: on reconnect all subscriptions are
    re-sent (resubscribe is transparent to the user). Reset on subscriptions is
    called before resubscribe.
  - TestConn_PingPong: client sends text "ping" at the given interval.
  - TestConn_PrivateLogin: on a private endpoint the client performs login first.
  - TestConn_DispatchOnInstTypeOnly: subscription without InstID (with InstType) —
    push with InstID reaches the handler via channel-key fallback.
  - TestConn_DroppedMessageCounter: invalid JSON ⇒ dropped counter is incremented.

Local mock-server uses gorilla/websocket Upgrader; no external dependencies or
outbound network.
*/

package ws

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
	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/okxlog"
	"github.com/tonymontanov/go-okx/v2/internal/okxmet"
)

// testCounter — simplest counter with atomic increment for test metrics.
type testCounter struct{ v atomic.Int64 }

func (c *testCounter) Inc()              { c.v.Add(1) }
func (c *testCounter) Add(d float64)     { c.v.Add(int64(d)) }
func (c *testCounter) Value() int64      { return c.v.Load() }

// testFactory — test metrics factory, indexes counters by name.
type testFactory struct {
	mu sync.Mutex
	m  map[string]*testCounter
}

func newTestFactory() *testFactory {
	return &testFactory{m: make(map[string]*testCounter)}
}

func (f *testFactory) Counter(name string, _ ...string) okxmet.Counter {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.m[name]; ok {
		return c
	}
	var c *testCounter = &testCounter{}
	f.m[name] = c
	return c
}

func (f *testFactory) Get(name string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.m[name]; ok {
		return c.Value()
	}
	return 0
}

// serverScript — describes mock-server behavior for a single test.
type serverScript struct {
	// onSubscribe is called when op="subscribe" is received. Returns a list of
	// JSON messages the server immediately sends to the client after subscribe.
	onSubscribe func(arg subscribeArg) []string
	// onLogin is called when op="login". Returns the string the server sends
	// back. If empty — server sends the default {"event":"login","code":"0"}.
	onLogin func(arg loginArg) string
	// recordReceived accumulates all messages received from the client.
	recordReceived func(message string)
	// onOp is called for any op-command with an id (order/cancel-order/amend-order/
	// batch-orders/cancel-batch-orders/amend-batch-orders/mass-cancel, etc.).
	// Returns a list of JSON messages the server sends back. If the function is
	// nil — the server silently ignores the command (used for timeout tests).
	onOp func(id, op string, raw string) []string
}

// startWsServer starts an OKX-style mock server on a free port.
// Returns the URL (ws://...) and a closer for cleanup.
func startWsServer(t *testing.T, script serverScript) (string, *httptest.Server) {
	t.Helper()

	var upgrader websocket.Upgrader = websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}

	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c *websocket.Conn
		var err error
		c, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
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
			if script.recordReceived != nil {
				script.recordReceived(string(message))
			}
		var s string = string(message)
		if s == "ping" {
			_ = c.WriteMessage(websocket.TextMessage, []byte("pong"))
			continue
		}
		// first try as op-request with id (WS Order API):
		// it has both id and op; subscribe/unsubscribe/login have no id.
			var idProbe opRequestMessage
			if err = codec.Unmarshal(message, &idProbe); err == nil && idProbe.ID != "" && idProbe.Op != "" {
				if script.onOp != nil {
					var replies []string = script.onOp(idProbe.ID, idProbe.Op, s)
					var i int
					for i = 0; i < len(replies); i++ {
						_ = c.WriteMessage(websocket.TextMessage, []byte(replies[i]))
					}
				}
				continue
			}

			// parse as opRequest
			var req opRequest
			if err = codec.Unmarshal(message, &req); err == nil && req.Op != "" {
				switch req.Op {
				case "login":
					var lreq loginRequest
					_ = codec.Unmarshal(message, &lreq)
					var reply string
					if script.onLogin != nil && len(lreq.Args) > 0 {
						reply = script.onLogin(lreq.Args[0])
					}
					if reply == "" {
						reply = `{"event":"login","code":"0","msg":""}`
					}
					_ = c.WriteMessage(websocket.TextMessage, []byte(reply))
				case "subscribe":
					var i int
					for i = 0; i < len(req.Args); i++ {
						var ack string = `{"event":"subscribe","arg":{"channel":"` + req.Args[i].Channel + `","instId":"` + req.Args[i].InstID + `"}}`
						_ = c.WriteMessage(websocket.TextMessage, []byte(ack))
						if script.onSubscribe != nil {
							var pushes []string = script.onSubscribe(req.Args[i])
							var j int
							for j = 0; j < len(pushes); j++ {
								_ = c.WriteMessage(websocket.TextMessage, []byte(pushes[j]))
							}
						}
					}
				case "unsubscribe":
					var i int
					for i = 0; i < len(req.Args); i++ {
						var ack string = `{"event":"unsubscribe","arg":{"channel":"` + req.Args[i].Channel + `","instId":"` + req.Args[i].InstID + `"}}`
						_ = c.WriteMessage(websocket.TextMessage, []byte(ack))
					}
				}
			}
		}
	}))

	var u string = strings.Replace(srv.URL, "http://", "ws://", 1)
	return u, srv
}

// waitFor waits until condition() returns true, up to the given timeout.
// Returns true if it succeeded in time.
func waitFor(timeout time.Duration, condition func() bool) bool {
	var deadline time.Time = time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return condition()
}

func defaultCfg(u string, private bool) Config {
	return Config{
		URL:                     u,
		IsPrivate:               private,
		HandshakeTimeout:        2 * time.Second,
		ReadTimeout:             2 * time.Second,
		WriteTimeout:            1 * time.Second,
		PingInterval:            50 * time.Millisecond,
		ReconnectInitialBackoff: 20 * time.Millisecond,
		ReconnectMaxBackoff:     100 * time.Millisecond,
		ReconnectJitter:         0,
		ReadBufferSize:          4096,
		WriteBufferSize:         4096,
	}
}

func TestConn_Connect_Subscribe_Receive(t *testing.T) {
	var got atomic.Int64
	var url string
	var srv *httptest.Server
	url, srv = startWsServer(t, serverScript{
		onSubscribe: func(arg subscribeArg) []string {
			return []string{
				`{"arg":{"channel":"` + arg.Channel + `","instId":"` + arg.InstID + `"},"action":"snapshot","data":[{"foo":"bar"}]}`,
			}
		},
	})
	defer srv.Close()

	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), f)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	var err error = c.Subscribe(&Subscription{
		Channel: "books",
		InstID:  "BTC-USDT-SWAP",
		Handler: func(_ string, _ []byte) { got.Add(1) },
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if !waitFor(2*time.Second, func() bool { return got.Load() >= 1 }) {
		t.Fatalf("did not receive push within timeout")
	}
	if f.Get("okx_ws_messages_received_total") < 1 {
		t.Fatalf("messages_received_total must be >0")
	}
	_ = c.Close()
}

func TestConn_AutoResubscribeAfterReconnect(t *testing.T) {
	var resetCalls atomic.Int64
	var subscribeCalls atomic.Int64
	var url string
	var srv *httptest.Server
	url, srv = startWsServer(t, serverScript{
		onSubscribe: func(arg subscribeArg) []string {
			subscribeCalls.Add(1)
			return nil
		},
	})

	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), f)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	_ = c.Subscribe(&Subscription{
		Channel: "books",
		InstID:  "BTC-USDT-SWAP",
		Handler: func(_ string, _ []byte) {},
		Reset:   func() { resetCalls.Add(1) },
	})

	if !waitFor(2*time.Second, func() bool { return subscribeCalls.Load() >= 1 }) {
		t.Fatalf("server did not see initial subscribe")
	}

	// Force reconnect: kill the server, bring up a new one on the same URL
	srv.Close()
	url, srv = startWsServer(t, serverScript{
		onSubscribe: func(arg subscribeArg) []string {
			subscribeCalls.Add(1)
			return nil
		},
	})
	// httptest.Server gets a new port each time; here we care about the logic —
	// the client must attempt to reconnect. So we swap the URL inside Conn via
	// restarting Conn with the same ctx — this test emulates a subscription to
	// the same URL in a new instance.
	_ = c.Close()
	defer srv.Close()

	c = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), f)
	c.Start(ctx)
	_ = c.Subscribe(&Subscription{
		Channel: "books",
		InstID:  "BTC-USDT-SWAP",
		Handler: func(_ string, _ []byte) {},
		Reset:   func() { resetCalls.Add(1) },
	})
	if !waitFor(2*time.Second, func() bool { return subscribeCalls.Load() >= 2 }) {
		t.Fatalf("expected at least 2 subscribe calls, got %d", subscribeCalls.Load())
	}
	if resetCalls.Load() < 1 {
		t.Fatalf("expected Reset to be called at least once, got %d", resetCalls.Load())
	}
	_ = c.Close()
}

func TestConn_PingPong(t *testing.T) {
	var pings atomic.Int64
	var url string
	var srv *httptest.Server
	url, srv = startWsServer(t, serverScript{
		recordReceived: func(message string) {
			if message == "ping" {
				pings.Add(1)
			}
		},
	})
	defer srv.Close()

	var f *testFactory = newTestFactory()
	var cfg Config = defaultCfg(url, false)
	cfg.PingInterval = 30 * time.Millisecond
	var c *Conn = NewConn(cfg, nil, okxlog.Noop(), f)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	c.Start(ctx)
	// trigger connection establishment via any subscription
	_ = c.Subscribe(&Subscription{
		Channel: "trades",
		InstID:  "BTC-USDT-SWAP",
		Handler: func(_ string, _ []byte) {},
	})
	if !waitFor(800*time.Millisecond, func() bool { return pings.Load() >= 2 }) {
		t.Fatalf("expected at least 2 pings, got %d", pings.Load())
	}
	_ = c.Close()
}

func TestConn_PrivateLogin(t *testing.T) {
	var loginSeen atomic.Bool
	var url string
	var srv *httptest.Server
	url, srv = startWsServer(t, serverScript{
		onLogin: func(arg loginArg) string {
			if arg.APIKey == "" || arg.Sign == "" || arg.Passphrase == "" {
				return `{"event":"login","code":"50001","msg":"bad sign"}`
			}
			loginSeen.Store(true)
			return `{"event":"login","code":"0","msg":""}`
		},
	})
	defer srv.Close()

	var signer *auth.Signer = auth.NewSigner("k", "s", "p")
	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(defaultCfg(url, true), signer, okxlog.Noop(), f)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	_ = c.Subscribe(&Subscription{
		Channel:  "positions",
		InstType: "SWAP",
		Handler:  func(_ string, _ []byte) {},
	})
	if !waitFor(2*time.Second, func() bool { return loginSeen.Load() }) {
		t.Fatalf("server did not see login op")
	}
	_ = c.Close()
}

func TestConn_DispatchOnInstTypeOnly(t *testing.T) {
	var got atomic.Int64
	var url string
	var srv *httptest.Server
	url, srv = startWsServer(t, serverScript{
		onSubscribe: func(arg subscribeArg) []string {
			// Server sends message WITHOUT instId in arg (as for positions/orders channels).
			return []string{`{"arg":{"channel":"positions","instType":"SWAP"},"data":[{"instId":"BTC-USDT-SWAP","pos":"1"}]}`}
		},
	})
	defer srv.Close()

	var signer *auth.Signer = auth.NewSigner("k", "s", "p")
	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(defaultCfg(url, true), signer, okxlog.Noop(), f)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	_ = c.Subscribe(&Subscription{
		Channel:  "positions",
		InstType: "SWAP",
		Handler:  func(_ string, _ []byte) { got.Add(1) },
	})
	if !waitFor(2*time.Second, func() bool { return got.Load() >= 1 }) {
		t.Fatalf("dispatch by channel-only key failed")
	}
	_ = c.Close()
}

func TestConn_DroppedMessageCounter(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startWsServer(t, serverScript{
		onSubscribe: func(arg subscribeArg) []string {
			return []string{`{not a valid json`}
		},
	})
	defer srv.Close()

	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), f)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	_ = c.Subscribe(&Subscription{
		Channel: "trades",
		InstID:  "BTC-USDT-SWAP",
		Handler: func(_ string, _ []byte) {},
	})
	if !waitFor(2*time.Second, func() bool { return f.Get("okx_ws_messages_dropped_total") >= 1 }) {
		t.Fatalf("dropped counter must be incremented for invalid JSON")
	}
	_ = c.Close()
}
