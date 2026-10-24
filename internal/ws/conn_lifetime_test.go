package ws

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tonymontanov/go-okx/v2/internal/okxlog"
)

// Регрессия 2026-10-22: супервизор жил на ctx первого вызвавшего Start.
// Его отмена (ядро переподписывало поток позиции) молча останавливала
// соединение для всех остальных подписчиков, а новые Subscribe без ошибки
// ложились в реестр «до коннекта», который уже не наступал.
func TestConn_SurvivesStartCtxCancel(t *testing.T) {
	var pushes atomic.Int64
	var url string
	var srv interface{ Close() }
	url, srv = startWsServer(t, serverScript{
		onSubscribe: func(arg subscribeArg) []string {
			return []string{
				`{"arg":{"channel":"` + arg.Channel + `","instId":"` + arg.InstID + `"},"data":[{"x":1}]}`,
			}
		},
	})
	defer srv.Close()

	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), f)
	defer func() { _ = c.Close() }()

	var firstCtx context.Context
	var cancelFirst context.CancelFunc
	firstCtx, cancelFirst = context.WithCancel(context.Background())
	c.Start(firstCtx)
	if err := c.Subscribe(&Subscription{
		Channel: "books", InstID: "BTC-USDT-SWAP",
		Handler: func(_ string, _ []byte) { pushes.Add(1) },
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return pushes.Load() >= 1 }) {
		t.Fatalf("first push not received")
	}

	// Первый подписчик ушёл — соединение общее и должно остаться.
	cancelFirst()
	time.Sleep(50 * time.Millisecond)

	c.mu.RLock()
	var alive bool = c.socket != nil
	c.mu.RUnlock()
	if !alive {
		t.Fatalf("socket dropped after the first caller's ctx was cancelled")
	}

	var before int64 = pushes.Load()
	if err := c.Subscribe(&Subscription{
		Channel: "trades", InstID: "BTC-USDT-SWAP",
		Handler: func(_ string, _ []byte) { pushes.Add(1) },
	}); err != nil {
		t.Fatalf("second subscribe: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return pushes.Load() > before }) {
		t.Fatalf("second subscription got no push: connection is dead for later callers")
	}
	if f.Get("okx_ws_reconnects_total") != 0 {
		t.Fatalf("unexpected reconnects: %d", f.Get("okx_ws_reconnects_total"))
	}
}

func TestConn_Reconnect_RedialsAndResubscribes(t *testing.T) {
	var mu sync.Mutex
	var subscribes int
	var pushes atomic.Int64
	var url string
	var srv interface{ Close() }
	url, srv = startWsServer(t, serverScript{
		recordReceived: func(m string) {
			if strings.Contains(m, `"op":"subscribe"`) {
				mu.Lock()
				subscribes++
				mu.Unlock()
			}
		},
		onSubscribe: func(arg subscribeArg) []string {
			return []string{
				`{"arg":{"channel":"` + arg.Channel + `","instId":"` + arg.InstID + `"},"data":[{"x":1}]}`,
			}
		},
	})
	defer srv.Close()

	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), f)
	defer func() { _ = c.Close() }()

	c.Start(context.Background())
	if err := c.Subscribe(&Subscription{
		Channel: "books", InstID: "BTC-USDT-SWAP",
		Handler: func(_ string, _ []byte) { pushes.Add(1) },
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return pushes.Load() >= 1 }) {
		t.Fatalf("first push not received")
	}

	if err := c.Reconnect("test: stream silent"); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if !waitFor(5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return subscribes >= 2
	}) {
		t.Fatalf("subscription was not resent after Reconnect (subscribes=%d)", subscribes)
	}
	if !waitFor(2*time.Second, func() bool { return pushes.Load() >= 2 }) {
		t.Fatalf("no push after Reconnect")
	}
	if f.Get("okx_ws_reconnect_requested_total") != 1 {
		t.Fatalf("reconnect_requested_total = %d, want 1", f.Get("okx_ws_reconnect_requested_total"))
	}
	if f.Get("okx_ws_reconnects_total") < 1 {
		t.Fatalf("reconnects_total must be >= 1")
	}
}

func TestConn_Reconnect_WithoutSocket_IsNoop(t *testing.T) {
	var c *Conn = NewConn(defaultCfg("ws://127.0.0.1:1", false), nil, okxlog.Noop(), newTestFactory())
	defer func() { _ = c.Close() }()
	if err := c.Reconnect("never connected"); err != nil {
		t.Fatalf("Reconnect without socket must be nil, got %v", err)
	}
}

func TestConn_SubscribeAfterClose_ReturnsErrConnClosed(t *testing.T) {
	var c *Conn = NewConn(defaultCfg("ws://127.0.0.1:1", false), nil, okxlog.Noop(), newTestFactory())
	_ = c.Close()
	var err error = c.Subscribe(&Subscription{Channel: "books", InstID: "BTC-USDT-SWAP", Handler: func(string, []byte) {}})
	if err != ErrConnClosed {
		t.Fatalf("Subscribe after Close: got %v, want ErrConnClosed", err)
	}
	err = c.SubscribeWithReset(&Subscription{Channel: "trades", InstID: "BTC-USDT-SWAP", Handler: func(string, []byte) {}})
	if err != ErrConnClosed {
		t.Fatalf("SubscribeWithReset after Close: got %v, want ErrConnClosed", err)
	}
	if err = c.Reconnect("after close"); err != ErrConnClosed {
		t.Fatalf("Reconnect after Close: got %v, want ErrConnClosed", err)
	}
	// Start после Close не должен поднимать супервизор.
	c.Start(context.Background())
	time.Sleep(20 * time.Millisecond)
	c.mu.RLock()
	var cancel context.CancelFunc = c.cancel
	c.mu.RUnlock()
	if cancel != nil {
		t.Fatalf("Start after Close must not install a supervisor")
	}
}
