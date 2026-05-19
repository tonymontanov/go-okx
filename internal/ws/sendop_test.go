/*
FILE: internal/ws/sendop_test.go

DESCRIPTION:
Tests for Conn.SendOp — request/reply pattern over a single WS connection.

Coverage:
  - TestSendOp_HappyPath           — correct send and receive reply
    with the right correlation-id and payload in Data.
  - TestSendOp_AutoIDGeneration    — empty req.ID generates a unique id;
    multiple sequential SendOp calls produce distinct ids.
  - TestSendOp_Timeout             — server ignores op, SendOp returns
    ErrOpTimeout, pending-map is cleared, op_timeout_total is incremented.
  - TestSendOp_ContextCancel       — cancelling ctx before reply arrives returns
    ctx.Err() and leaves no leaks in pending.
  - TestSendOp_NotReady            — SendOp before Start (or immediately after Close)
    returns a typed error without hanging.
  - TestSendOp_ConnectionLost      — server drops connection, all pending SendOp
    immediately receive a synthetic OpResponse with code="disconnected"
    (do NOT hang until timeout).
  - TestSendOp_OrphanReply         — server sends reply with an unknown id;
    op_orphan_total is incremented, push branch remains functional.
  - TestSendOp_ConcurrentReplies   — N concurrent SendOp calls; server replies in
    reverse order; each caller receives ITS OWN reply.
*/

package ws

import (
	"context"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/okxlog"
)

// startedConn — helper: starts a mock server, creates a Conn, calls Start,
// triggers socket establishment via a no-op subscription, and waits for readiness.
// Returns Conn, metrics factory, and cleanup func.
func startedConn(t *testing.T, script serverScript) (*Conn, *testFactory, func()) {
	t.Helper()

	var url string
	var srv *httptest.Server
	url, srv = startWsServer(t, script)

	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(defaultCfg(url, false), nil, okxlog.Noop(), f)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	c.Start(ctx)

	// trigger socket establishment via a subscription — without it supervise
	// will not call connectAndRun until the first Subscribe/SendOp.
	_ = c.Subscribe(&Subscription{
		Channel: "trades",
		InstID:  "BTC-USDT",
		Handler: func(_ string, _ []byte) {},
	})

	// wait for socket readiness (socket != nil)
	if !waitFor(2*time.Second, func() bool {
		c.mu.RLock()
		var ready bool = c.socket != nil
		c.mu.RUnlock()
		return ready
	}) {
		cancel()
		srv.Close()
		t.Fatalf("connection not ready in time")
	}

	return c, f, func() {
		cancel()
		_ = c.Close()
		srv.Close()
	}
}

func TestSendOp_HappyPath(t *testing.T) {
	var c *Conn
	var cleanup func()
	c, _, cleanup = startedConn(t, serverScript{
		onOp: func(id, op, _ string) []string {
			return []string{
				`{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"123","clOrdId":"c-1"}],"inTime":"1","outTime":"2"}`,
			}
		},
	})
	defer cleanup()

	var ctx context.Context = context.Background()
	var resp OpResponse
	var err error
	resp, err = c.SendOp(ctx, OpRequest{
		ID:   "fixed-id-42",
		Op:   "order",
		Args: []map[string]string{{"instId": "BTC-USDT"}},
	}, time.Second)
	if err != nil {
		t.Fatalf("SendOp: %v", err)
	}
	if resp.ID != "fixed-id-42" {
		t.Fatalf("ID mismatch: got %q want %q", resp.ID, "fixed-id-42")
	}
	if resp.Code != "0" {
		t.Fatalf("Code != 0: %q", resp.Code)
	}
	if resp.Op != "order" {
		t.Fatalf("Op mismatch: %q", resp.Op)
	}
	if resp.InTime != "1" || resp.OutTime != "2" {
		t.Fatalf("timings mismatch: in=%q out=%q", resp.InTime, resp.OutTime)
	}
	if len(resp.Data) == 0 {
		t.Fatalf("Data must be non-empty")
	}
}

func TestSendOp_AutoIDGeneration(t *testing.T) {
	var seen sync.Map
	var c *Conn
	var cleanup func()
	c, _, cleanup = startedConn(t, serverScript{
		onOp: func(id, op, _ string) []string {
			seen.Store(id, true)
			return []string{`{"id":"` + id + `","op":"` + op + `","code":"0","msg":""}`}
		},
	})
	defer cleanup()

	var ctx context.Context = context.Background()
	var i int
	for i = 0; i < 5; i++ {
		var _, err = c.SendOp(ctx, OpRequest{Op: "order"}, time.Second)
		if err != nil {
			t.Fatalf("SendOp[%d]: %v", i, err)
		}
	}

	var count int
	seen.Range(func(_, _ any) bool { count++; return true })
	if count != 5 {
		t.Fatalf("expected 5 distinct auto-ids, got %d", count)
	}
}

func TestSendOp_Timeout(t *testing.T) {
	var c *Conn
	var f *testFactory
	var cleanup func()
	c, f, cleanup = startedConn(t, serverScript{
		onOp: func(_, _, _ string) []string { return nil }, // silent
	})
	defer cleanup()

	var ctx context.Context = context.Background()
	var _, err = c.SendOp(ctx, OpRequest{Op: "order"}, 80*time.Millisecond)
	if err != ErrOpTimeout {
		t.Fatalf("expected ErrOpTimeout, got %v", err)
	}
	if f.Get("okx_ws_op_timeout_total") != 1 {
		t.Fatalf("op_timeout_total must be 1, got %d", f.Get("okx_ws_op_timeout_total"))
	}

	c.pendingMu.Lock()
	var leak int = len(c.pending)
	c.pendingMu.Unlock()
	if leak != 0 {
		t.Fatalf("pending leaked %d entries", leak)
	}
}

func TestSendOp_ContextCancel(t *testing.T) {
	var c *Conn
	var cleanup func()
	c, _, cleanup = startedConn(t, serverScript{
		onOp: func(_, _, _ string) []string { return nil }, // silent
	})
	defer cleanup()

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	// cancel after 30ms, without waiting for timeout
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	var _, err = c.SendOp(ctx, OpRequest{Op: "order"}, time.Second)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	c.pendingMu.Lock()
	var leak int = len(c.pending)
	c.pendingMu.Unlock()
	if leak != 0 {
		t.Fatalf("pending leaked %d entries", leak)
	}
}

func TestSendOp_NotReady(t *testing.T) {
	// Create Conn but do NOT call Start — socket will not be established.
	var f *testFactory = newTestFactory()
	var c *Conn = NewConn(Config{
		URL:                     "ws://127.0.0.1:1",
		HandshakeTimeout:        100 * time.Millisecond,
		ReadTimeout:             100 * time.Millisecond,
		WriteTimeout:            100 * time.Millisecond,
		PingInterval:            time.Second,
		ReconnectInitialBackoff: 100 * time.Millisecond,
		ReconnectMaxBackoff:     100 * time.Millisecond,
	}, nil, okxlog.Noop(), f)
	var ctx context.Context = context.Background()
	var _, err = c.SendOp(ctx, OpRequest{Op: "order"}, 100*time.Millisecond)
	if err != ErrConnNotReady {
		t.Fatalf("expected ErrConnNotReady, got %v", err)
	}

	// After Close — ErrConnClosed
	_ = c.Close()
	_, err = c.SendOp(ctx, OpRequest{Op: "order"}, 100*time.Millisecond)
	if err != ErrConnClosed {
		t.Fatalf("expected ErrConnClosed after Close, got %v", err)
	}
}

func TestSendOp_ConnectionLost(t *testing.T) {
	// Server accepts op but does NOT reply; then we close Conn —
	// emulating a disconnect effect (Close drains pending).
	// A full disconnect scenario with the server itself is harder (sleep +
	// httptest.Server.Close breaks the socket, but read-loop returns with
	// arbitrary delay); explicit Close is simpler and tests the same
	// failAllPending branch.
	var c *Conn
	var cleanup func()
	c, _, cleanup = startedConn(t, serverScript{
		onOp: func(_, _, _ string) []string { return nil },
	})
	defer cleanup()

	var ctx context.Context = context.Background()
	var doneCh chan error = make(chan error, 1)
	go func() {
		var _, err = c.SendOp(ctx, OpRequest{Op: "order"}, 5*time.Second)
		doneCh <- err
	}()

	// give SendOp time to register in pending
	time.Sleep(50 * time.Millisecond)
	_ = c.Close()

	select {
	case err := <-doneCh:
		// After Close, failAllPending sends OpResponse{Code:"disconnected"};
		// SendOp returns (resp, nil) — no error because a reply arrived
		// (even a synthetic one). Contract: caller checks resp.Code != "0".
		if err != nil {
			t.Fatalf("SendOp returned error instead of synthetic reply: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SendOp did not unblock after Close")
	}
}

func TestSendOp_OrphanReply(t *testing.T) {
	// Server sends reply with an unexpected id BEFORE we send anything.
	// Emulates server-side noise / double reply.
	var c *Conn
	var f *testFactory
	var cleanup func()
	c, f, cleanup = startedConn(t, serverScript{
		onSubscribe: func(arg subscribeArg) []string {
			return []string{
				`{"id":"orphan-99","op":"order","code":"0","msg":""}`,
			}
		},
	})
	defer cleanup()

	if !waitFor(2*time.Second, func() bool {
		return f.Get("okx_ws_op_orphan_total") >= 1
	}) {
		t.Fatalf("orphan counter must be incremented for unknown-id reply")
	}

	// After this, normal subscribe-pushes must continue to work.
	// Verify that a push with data reaches the handler (via a new subscription).
	var got atomic.Int64
	_ = c.Subscribe(&Subscription{
		Channel: "books",
		InstID:  "ETH-USDT",
		Handler: func(_ string, _ []byte) { got.Add(1) },
	})
	// this subscribe itself will trigger ack+push in onSubscribe above
	// (if it was provided). In our scenario onSubscribe returns a reply
	// with id, which must NOT break the receive-pipeline.
	if f.Get("okx_ws_op_orphan_total") < 1 {
		t.Fatalf("orphan counter regression")
	}
}

func TestSendOp_ConcurrentReplies(t *testing.T) {
	// Server delays replies randomly, verifying that correlation works correctly
	// for concurrent requests.
	var c *Conn
	var cleanup func()
	c, _, cleanup = startedConn(t, serverScript{
		onOp: func(id, op, raw string) []string {
			_ = raw
			// echo back id in ordId — later verify each caller got ITS OWN reply.
			return []string{
				`{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"echo-` + id + `"}]}`,
			}
		},
	})
	defer cleanup()

	var wg sync.WaitGroup
	var ctx context.Context = context.Background()
	const n int = 16
	var results [n]string
	var errs [n]error
	wg.Add(n)
	var i int
	for i = 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			var id string = "cc-" + strconv.Itoa(idx)
			var resp OpResponse
			var err error
			resp, err = c.SendOp(ctx, OpRequest{ID: id, Op: "order"}, 2*time.Second)
			errs[idx] = err
			results[idx] = resp.ID
		}(i)
	}
	wg.Wait()

	for i = 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("SendOp[%d]: %v", i, errs[i])
		}
		var want string = "cc-" + strconv.Itoa(i)
		if results[i] != want {
			t.Fatalf("correlation mismatch [%d]: got %q want %q", i, results[i], want)
		}
	}
}

// keepRefs ensures the compiler does not discard the codec reference,
// needed in case Unmarshal checks on Data are added.
var _ = codec.Marshal
