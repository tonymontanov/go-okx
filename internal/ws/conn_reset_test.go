/*
FILE: internal/ws/conn_reset_test.go

DESCRIPTION:
Tests for SubscribeWithReset: Reset must run exactly once per connection the
subscription is sent on — on connect when registered before the socket is up,
and synchronously (before the subscribe command goes out) when registered on an
already connected socket. Plain Subscribe keeps its old behaviour: no Reset on
an already connected socket.
*/

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

func TestConn_SubscribeWithReset_BeforeConnect_ResetOnConnect(t *testing.T) {
	var resetCalls atomic.Int64
	var subscribeCalls atomic.Int64
	var url string
	var srv = startWsServerPtr(t, serverScript{
		onSubscribe: func(arg subscribeArg) []string {
			subscribeCalls.Add(1)
			return nil
		},
	}, &url)
	defer srv.Close()

	var c *Conn = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), newTestFactory())
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Registered BEFORE Start: the socket is down, Reset must wait for connect.
	var err error = c.SubscribeWithReset(&Subscription{
		Channel: "orders",
		InstID:  "BTC-USDT",
		Handler: func(_ string, _ []byte) {},
		Reset:   func() { resetCalls.Add(1) },
	})
	if err != nil {
		t.Fatalf("SubscribeWithReset: %v", err)
	}
	if resetCalls.Load() != 0 {
		t.Fatalf("Reset ran before the socket was connected: %d", resetCalls.Load())
	}
	c.Start(ctx)
	if !waitFor(2*time.Second, func() bool { return subscribeCalls.Load() >= 1 }) {
		t.Fatalf("server did not see the subscribe")
	}
	if resetCalls.Load() != 1 {
		t.Fatalf("Reset calls = %d, want exactly 1 (on connect)", resetCalls.Load())
	}
	_ = c.Close()
}

func TestConn_SubscribeWithReset_AlreadyConnected_ResetBeforeSubscribe(t *testing.T) {
	var mu sync.Mutex
	var seq []string
	var record = func(s string) {
		mu.Lock()
		seq = append(seq, s)
		mu.Unlock()
	}
	var sawOrders atomic.Bool
	var sawBooks atomic.Bool
	var url string
	var srv = startWsServerPtr(t, serverScript{
		recordReceived: func(message string) {
			if !strings.Contains(message, `"op":"subscribe"`) {
				return
			}
			if strings.Contains(message, `"channel":"orders"`) {
				record("subscribe:orders")
				sawOrders.Store(true)
			}
			if strings.Contains(message, `"channel":"books"`) {
				sawBooks.Store(true)
			}
		},
	}, &url)
	defer srv.Close()

	var c *Conn = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), newTestFactory())
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	// Warm-up subscription: once the server has seen it, the socket is up.
	var plainReset atomic.Int64
	_ = c.Subscribe(&Subscription{
		Channel: "books",
		InstID:  "BTC-USDT",
		Handler: func(_ string, _ []byte) {},
		Reset:   func() { plainReset.Add(1) },
	})
	if !waitFor(2*time.Second, func() bool { return sawBooks.Load() }) {
		t.Fatalf("server did not see the warm-up subscribe")
	}
	var plainResetsAtConnect int64 = plainReset.Load()

	// Plain Subscribe on a connected socket: no Reset (old behaviour kept).
	_ = c.Subscribe(&Subscription{
		Channel: "trades",
		InstID:  "BTC-USDT",
		Handler: func(_ string, _ []byte) {},
		Reset:   func() { plainReset.Add(1) },
	})
	if plainReset.Load() != plainResetsAtConnect {
		t.Fatalf("plain Subscribe must not call Reset on a connected socket")
	}

	// SubscribeWithReset on a connected socket: Reset synchronously, before the
	// subscribe command is written.
	var resetSeen atomic.Bool
	var err error = c.SubscribeWithReset(&Subscription{
		Channel: "orders",
		InstID:  "BTC-USDT",
		Handler: func(_ string, _ []byte) {},
		Reset: func() {
			resetSeen.Store(true)
			record("reset")
		},
	})
	if err != nil {
		t.Fatalf("SubscribeWithReset: %v", err)
	}
	if !resetSeen.Load() {
		t.Fatalf("Reset must run synchronously inside SubscribeWithReset on a connected socket")
	}
	if !waitFor(2*time.Second, func() bool { return sawOrders.Load() }) {
		t.Fatalf("server did not see the orders subscribe")
	}
	mu.Lock()
	var got []string = append([]string(nil), seq...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "reset" || got[1] != "subscribe:orders" {
		t.Fatalf("sequence = %v, want [reset subscribe:orders]", got)
	}
	_ = c.Close()
}

// startWsServerPtr — startWsServer whose URL lands in *url (keeps the two-value
// helper untouched while letting the deferred Close sit next to the call).
func startWsServerPtr(t *testing.T, script serverScript, url *string) interface{ Close() } {
	t.Helper()
	var u string
	var srv interface{ Close() }
	u, srv = startWsServer(t, script)
	*url = u
	return srv
}
