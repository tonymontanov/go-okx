/*
FILE: internal/ws/conn.go

DESCRIPTION:
conn.go implements a managing wrapper over a single OKX WebSocket connection.
At most two such objects are created per Client SDK:
  - one for the PUBLIC endpoint (no login, channels: books/trades/tickers/...);
  - one for the PRIVATE endpoint (with login, channels: orders/positions/account).

RESPONSIBILITIES:
  - connect / reconnect with backoff+jitter;
  - login (private only);
  - heartbeat (send text "ping" every PingInterval seconds);
  - subscribe / unsubscribe (with buffering: subscription is registered in the
    registry even if the socket is temporarily disconnected, applied on connect);
  - resubscribe after reconnect (fully transparent to the caller);
  - dispatch incoming push messages to subscription handlers;
  - graceful shutdown via ctx.

INTERNAL STATE MODEL:
  - subs (map subscriptionKey → *Subscription) — current registry.
  - socket — current *websocket.Conn (or nil when the connection is down).
    All RW protected by mu.
  - writeMu — separate mutex for write operations (gorilla/websocket requires
    exclusive writes).
  - runOnce / cancel — lifecycle management of background goroutines (read-loop,
    ping-loop). Goroutine restarts happen INSIDE the self-loop, invisible outside.

ERROR STRATEGY:
  - Local / transient errors (ReadMessage err) — transition to reconnect.
  - If Conn.Close() was called — read-loop exits silently.
  - Login or subscribe errors (event=error with code != "0") — logged and
    counted in metrics ws_login_failed_total / ws_subscribe_failed_total.
    The reconnect cycle continues (we do not "kill" Conn on the first login
    error to avoid the "server blinked — client died forever" scenario).
*/

package ws

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/okxerr"
	"github.com/tonymontanov/go-okx/v2/internal/okxlog"
	"github.com/tonymontanov/go-okx/v2/internal/okxmet"
)

// ErrConnClosed is returned when SendOp is called on an already closed Conn.
var ErrConnClosed = errors.New("ws: connection closed")

// ErrConnNotReady is returned when SendOp is called before the socket is
// established (Start has not yet connected or the socket is in reconnect-backoff).
var ErrConnNotReady = errors.New("ws: connection not ready")

// ErrOpTimeout is returned when a reply to an op-command did not arrive within
// the allotted time.
var ErrOpTimeout = errors.New("ws: op-request timeout")

// Subscription describes a single subscription. Fields are set by the caller
// (swap/stream.go).
type Subscription struct {
	// Channel — OKX channel name ("books", "bbo-tbt", "mark-price", "trades",
	// "positions", "orders", etc.).
	Channel string
	// InstID — instrument. Required for per-instrument channels; may be empty for
	// account channels (positions/orders) ⇒ instType is used instead.
	InstID string
	// InstType — instrument type ("SWAP"); used when InstID is empty.
	InstType string
	// Handler is called for each push message. payload is the content of the
	// data field (array!). action — "snapshot" / "update" (empty for non-books).
	Handler func(action string, payload []byte)
	// Reset is called before resubscribe (after reconnect). Used, for example,
	// by OrderbookEngine to reset the local order book before a new snapshot.
	Reset func()
}

func (s *Subscription) key() string {
	if s.InstID != "" {
		return s.Channel + ":" + s.InstID
	}
	if s.InstType != "" {
		return s.Channel + ":@" + s.InstType
	}
	return s.Channel
}

// Config — parameters for a single WS connection. Derived from the public okx.WsConfig.
type Config struct {
	URL                     string
	IsPrivate               bool
	HandshakeTimeout        time.Duration
	ReadTimeout             time.Duration
	WriteTimeout            time.Duration
	PingInterval            time.Duration
	ReconnectInitialBackoff time.Duration
	ReconnectMaxBackoff     time.Duration
	ReconnectJitter         float64
	ReadBufferSize          int
	WriteBufferSize         int
}

// Conn — managing wrapper over a single OKX WS connection.
type Conn struct {
	cfg     Config
	signer  *auth.Signer
	logger  okxlog.Logger
	metrics okxmet.CounterFactory

	mu      sync.RWMutex
	subs    map[string]*Subscription
	socket  *websocket.Conn
	writeMu sync.Mutex
	closed  bool
	cancel  context.CancelFunc

	startOnce sync.Once

	// pendingMu protects pending. Intentionally separate from mu (which protects
	// subs/socket/closed) — this allows SendOp to register pending entries without
	// blocking the read-loop during connect.
	pendingMu sync.Mutex
	pending   map[string]chan OpResponse

	// opIDSeq — atomic counter for generating correlation ids when the caller
	// did not provide one. Starts at 1 and increments.
	opIDSeq uint64

	cReceived okxmet.Counter
	cDropped  okxmet.Counter
	cReconn   okxmet.Counter
	cSub      okxmet.Counter
	cPingErr  okxmet.Counter
	cOpSent   okxmet.Counter
	cOpReply  okxmet.Counter
	cOpTmout  okxmet.Counter
	cOpOrphan okxmet.Counter
}

// NewConn creates a Conn. No network activity starts at this point —
// background goroutines are started on the first call to Start or Subscribe.
func NewConn(cfg Config, signer *auth.Signer, log okxlog.Logger, mf okxmet.CounterFactory) *Conn {
	if log == nil {
		log = okxlog.Noop()
	}
	if mf == nil {
		mf = okxmet.Noop()
	}
	var endpoint string = "public"
	if cfg.IsPrivate {
		endpoint = "private"
	}
	return &Conn{
		cfg:       cfg,
		signer:    signer,
		logger:    log,
		metrics:   mf,
		subs:      make(map[string]*Subscription, 16),
		pending:   make(map[string]chan OpResponse, 16),
		cReceived: mf.Counter("okx_ws_messages_received_total", "endpoint", endpoint),
		cDropped:  mf.Counter("okx_ws_messages_dropped_total", "endpoint", endpoint),
		cReconn:   mf.Counter("okx_ws_reconnects_total", "endpoint", endpoint),
		cSub:      mf.Counter("okx_ws_subscriptions_total", "endpoint", endpoint),
		cPingErr:  mf.Counter("okx_ws_ping_failed_total", "endpoint", endpoint),
		cOpSent:   mf.Counter("okx_ws_op_sent_total", "endpoint", endpoint),
		cOpReply:  mf.Counter("okx_ws_op_reply_total", "endpoint", endpoint),
		cOpTmout:  mf.Counter("okx_ws_op_timeout_total", "endpoint", endpoint),
		cOpOrphan: mf.Counter("okx_ws_op_orphan_total", "endpoint", endpoint),
	}
}

// Start starts the background supervisor loop (connect→loop→reconnect). Returns
// immediately; connection errors go to logs and metrics. Stops on ctx cancellation.
func (c *Conn) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		var supCtx context.Context
		supCtx, c.cancel = context.WithCancel(ctx)
		go c.supervise(supCtx)
	})
}

/*
EnsureReady idempotently calls Start(ctx) and blocks until the socket is
established (for private — until login completes).
Returns error on ctx.Done() or ErrConnClosed.

Used by domain layers (e.g. WS Order API) before the first SendOp: without
explicit readiness, SendOp would return ErrConnNotReady; in HFT scenarios it is
better to wait at startup than to run backoff-retry in caller code.

Note: "ready" = socket != nil. For a public connection this guarantees that
push messages can already arrive; for private — that login has completed
(login is done SYNCHRONOUSLY inside connectAndRun before writing socket to c.socket).
*/
func (c *Conn) EnsureReady(ctx context.Context) error {
	c.Start(ctx)

	var ticker *time.Ticker = time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		c.mu.RLock()
		var closed bool = c.closed
		var ready bool = c.socket != nil
		c.mu.RUnlock()
		if closed {
			return ErrConnClosed
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Subscribe adds a subscription to the registry and, if the connection is
// established, immediately sends the subscribe command. After reconnect the
// same subscription is automatically restored.
func (c *Conn) Subscribe(sub *Subscription) error {
	if sub == nil || sub.Channel == "" || sub.Handler == nil {
		return okxerr.New(okxerr.ErrorKindInvalidRequest, "", "ws: invalid subscription", nil)
	}
	c.mu.Lock()
	c.subs[sub.key()] = sub
	var socket *websocket.Conn = c.socket
	c.mu.Unlock()
	c.cSub.Inc()

	if socket == nil {
		return nil // will send on connect
	}
	return c.sendSubscribe(socket, sub)
}

// Unsubscribe removes the subscription from the registry and (if a socket exists) sends unsubscribe.
func (c *Conn) Unsubscribe(channel, instID string) error {
	var key string
	if instID != "" {
		key = channel + ":" + instID
	} else {
		key = channel
	}
	c.mu.Lock()
	var sub *Subscription = c.subs[key]
	delete(c.subs, key)
	var socket *websocket.Conn = c.socket
	c.mu.Unlock()

	if sub == nil || socket == nil {
		return nil
	}
	return c.sendUnsubscribe(socket, sub)
}

// Close gracefully shuts down Conn (cancels the supervise goroutine and closes
// the current socket). Safe to call multiple times.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	var s *websocket.Conn = c.socket
	c.socket = nil
	c.mu.Unlock()

	c.failAllPending(ErrConnClosed)

	if s != nil {
		_ = s.Close()
	}
	return nil
}

/*
SendOp sends an op-command (order/cancel-order/amend-order/batch-orders/
cancel-batch-orders/amend-batch-orders/mass-cancel) and waits for a reply.

BEHAVIOR:
  - If req.ID is empty — a unique monotonically-increasing id is generated
    (atomic counter).
  - The pending channel is registered BEFORE writeMessage so the read-loop
    can find it even on a very fast server reply.
  - Terminates on one of: reply received | ctx.Done | timeout elapsed
    (if timeout > 0). In all cases the entry is removed from pending-map (defer).
  - On an already closed Conn, returns ErrConnClosed.
  - If the socket is not yet established (Start not called or between
    reconnect backoffs) — returns ErrConnNotReady. This is intentional: op-
    commands do NOT survive reconnect (unlike subscriptions), and "magically
    buffering" them would be dangerous for HFT scenarios.

RETURN VALUE:
OpResponse contains top-level code/msg/data + inTime/outTime. Top-level
code != "0" means the command was rejected entirely (e.g. 60012
"Illegal request"). Per-item errors (sCode/sMsg inside Data) are handled by
the domain layer.
*/
func (c *Conn) SendOp(ctx context.Context, req OpRequest, timeout time.Duration) (OpResponse, error) {
	var empty OpResponse
	if req.Op == "" {
		return empty, okxerr.New(okxerr.ErrorKindInvalidRequest, "", "ws: empty op", nil)
	}

	c.mu.RLock()
	var closed bool = c.closed
	var socket *websocket.Conn = c.socket
	c.mu.RUnlock()
	if closed {
		return empty, ErrConnClosed
	}
	if socket == nil {
		return empty, ErrConnNotReady
	}

	if req.ID == "" {
		req.ID = strconv.FormatUint(atomic.AddUint64(&c.opIDSeq, 1), 10)
	}

	var ch chan OpResponse = make(chan OpResponse, 1)
	c.pendingMu.Lock()
	if _, exists := c.pending[req.ID]; exists {
		c.pendingMu.Unlock()
		return empty, okxerr.New(okxerr.ErrorKindInvalidRequest, "", "ws: duplicate op id", nil)
	}
	c.pending[req.ID] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, req.ID)
		c.pendingMu.Unlock()
	}()

	var wire opRequestMessage = opRequestMessage{
		ID:   req.ID,
		Op:   req.Op,
		Args: req.Args,
	}
	var raw []byte
	var err error
	raw, err = codec.Marshal(wire)
	if err != nil {
		return empty, fmt.Errorf("marshal op: %w", err)
	}
	if err = c.writeMessage(socket, raw); err != nil {
		return empty, fmt.Errorf("write op: %w", err)
	}
	c.cOpSent.Inc()

	if timeout <= 0 {
		// Pure ctx-driven wait.
		select {
		case resp := <-ch:
			return resp, nil
		case <-ctx.Done():
			return empty, ctx.Err()
		}
	}

	var timer *time.Timer = time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return empty, ctx.Err()
	case <-timer.C:
		c.cOpTmout.Inc()
		return empty, ErrOpTimeout
	}
}

// failAllPending delivers a synthetic OpResponse with the given error to all
// waiting SendOp callers, then clears the pending map. Used on reconnect and
// Close: op-commands do not survive a connection drop, and callers must receive
// an explicit response rather than hanging until timeout.
func (c *Conn) failAllPending(err error) {
	c.pendingMu.Lock()
	if len(c.pending) == 0 {
		c.pendingMu.Unlock()
		return
	}
	var failed map[string]chan OpResponse = c.pending
	c.pending = make(map[string]chan OpResponse, 16)
	c.pendingMu.Unlock()

	var msg string = ""
	if err != nil {
		msg = err.Error()
	}
	for id, ch := range failed {
		// Non-blocking: chan buffered=1; if someone already unsubscribed —
		// just discard.
		select {
		case ch <- OpResponse{ID: id, Code: "disconnected", Msg: msg}:
		default:
		}
	}
}

// supervise — main supervisor loop. Connects → reads → on error does backoff
// and retries. Stops on ctx.Done.
func (c *Conn) supervise(ctx context.Context) {
	var backoff time.Duration = c.cfg.ReconnectInitialBackoff
	var attempt int
	for {
		if ctx.Err() != nil {
			return
		}

		var err error = c.connectAndRun(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.logger.Warn("ws: connection error, will reconnect",
				okxlog.Str("url", c.cfg.URL),
				okxlog.Int("attempt", int64(attempt)),
				okxlog.Err(err),
			)
		}
		c.cReconn.Inc()
		attempt++

		var sleep time.Duration = applyJitter(backoff, c.cfg.ReconnectJitter)
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
		backoff = nextBackoff(backoff, c.cfg.ReconnectMaxBackoff)
	}
}

// connectAndRun establishes the connection, performs login (if private), sends
// all current subscriptions, starts ping-loop and read-loop, waits for them to
// finish. Returns the error used by supervise to decide whether to backoff.
func (c *Conn) connectAndRun(ctx context.Context) error {
	var dialer *websocket.Dialer = &websocket.Dialer{
		HandshakeTimeout: c.cfg.HandshakeTimeout,
		ReadBufferSize:   c.cfg.ReadBufferSize,
		WriteBufferSize:  c.cfg.WriteBufferSize,
	}
	var socket *websocket.Conn
	var err error
	socket, _, err = dialer.DialContext(ctx, c.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	c.logger.Info("ws: connected", okxlog.Str("url", c.cfg.URL))

	c.mu.Lock()
	c.socket = socket
	var subsCopy []*Subscription = make([]*Subscription, 0, len(c.subs))
	for _, s := range c.subs {
		if s.Reset != nil {
			s.Reset()
		}
		subsCopy = append(subsCopy, s)
	}
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.socket == socket {
			c.socket = nil
		}
		c.mu.Unlock()
		// op-commands do NOT survive reconnect: fail all pending with a
		// synthetic response so callers don't hang until timeout.
		c.failAllPending(errors.New("ws: connection lost"))
		_ = socket.Close()
	}()

	if c.cfg.IsPrivate {
		err = c.performLogin(ctx, socket)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
	}

	var i int
	for i = 0; i < len(subsCopy); i++ {
		if err = c.sendSubscribe(socket, subsCopy[i]); err != nil {
			c.logger.Warn("ws: resubscribe failed", okxlog.Str("channel", subsCopy[i].Channel), okxlog.Err(err))
		}
	}

	var loopCtx context.Context
	var loopCancel context.CancelFunc
	loopCtx, loopCancel = context.WithCancel(ctx)
	defer loopCancel()

	var wg sync.WaitGroup
	wg.Add(2)
	var readErr error
	go func() {
		defer wg.Done()
		defer loopCancel()
		readErr = c.readLoop(loopCtx, socket)
	}()
	go func() {
		defer wg.Done()
		c.pingLoop(loopCtx, socket)
	}()
	wg.Wait()

	if readErr != nil {
		return readErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// performLogin sends the login command and waits for a positive "login" event
// with code=="0". Reads up to ~3 seconds worth of frames to handle intermediate
// pong/heartbeat frames.
func (c *Conn) performLogin(ctx context.Context, socket *websocket.Conn) error {
	if c.signer == nil || !c.signer.Enabled() {
		return errors.New("ws: private endpoint requires signer with credentials")
	}
	var timestamp string = epochSecondsString()
	var req loginRequest
	var err error
	req, err = buildLogin(c.signer, timestamp)
	if err != nil {
		return err
	}

	var raw []byte
	raw, err = codec.Marshal(req)
	if err != nil {
		return err
	}
	if err = c.writeMessage(socket, raw); err != nil {
		return err
	}

	var deadline time.Time = time.Now().Add(3 * time.Second)
	_ = socket.SetReadDeadline(deadline)
	defer func() { _ = socket.SetReadDeadline(time.Time{}) }()

	var i int
	for i = 0; i < 10; i++ {
		var msgType int
		var message []byte
		msgType, message, err = socket.ReadMessage()
		if err != nil {
			return err
		}
		if msgType != websocket.TextMessage {
			continue
		}
		if string(message) == "pong" {
			continue
		}
		var env pushEnvelope
		if err = codec.Unmarshal(message, &env); err != nil {
			continue
		}
		if env.Event == "login" {
			if env.Code == "0" {
				c.logger.Info("ws: login ok")
				return nil
			}
			return fmt.Errorf("login rejected: code=%s msg=%q", env.Code, env.Msg)
		}
		_ = ctx
	}
	return errors.New("ws: login response not received")
}

// readLoop reads messages and dispatches them to subscription handlers.
// Returns the error used by supervise to decide whether to reconnect.
func (c *Conn) readLoop(ctx context.Context, socket *websocket.Conn) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		_ = socket.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
		var msgType int
		var message []byte
		var err error
		msgType, message, err = socket.ReadMessage()
		if err != nil {
			return err
		}
		if msgType != websocket.TextMessage {
			continue
		}
		c.cReceived.Inc()

		if string(message) == "pong" {
			continue
		}

		var env pushEnvelope
		if err = codec.Unmarshal(message, &env); err != nil {
			c.cDropped.Inc()
			c.logger.Warn("ws: failed to parse envelope", okxlog.Err(err))
			continue
		}

		if env.Event != "" {
			c.handleEvent(env)
			continue
		}

		// op-replies have a non-empty id echoed back from the request.
		// Push messages carry no id, so the branches do not overlap.
		if env.ID != "" {
			c.dispatchOpReply(env)
			continue
		}

		if env.Arg.Channel == "" || len(env.Data) == 0 {
			c.cDropped.Inc()
			continue
		}

		var key string = env.Arg.Channel + ":" + env.Arg.InstID
		if env.Arg.InstID == "" {
			key = env.Arg.Channel + ":@" + env.Arg.InstType
		}
		c.mu.RLock()
		var sub *Subscription = c.subs[key]
		if sub == nil {
			// fallback: orders/positions subscriptions may arrive without instId
			sub = c.subs[env.Arg.Channel]
		}
		c.mu.RUnlock()
		if sub == nil {
			c.cDropped.Inc()
			continue
		}
		sub.Handler(env.Action, env.Data)
	}
}

// dispatchOpReply looks up the pending channel by id and delivers an OpResponse.
// If no pending entry is found — the reply is "orphaned" (caller timed out or
// cancelled ctx earlier): increment counter and silently discard.
func (c *Conn) dispatchOpReply(env incomingEnvelope) {
	c.pendingMu.Lock()
	var ch chan OpResponse = c.pending[env.ID]
	c.pendingMu.Unlock()

	if ch == nil {
		c.cOpOrphan.Inc()
		return
	}
	var resp OpResponse = OpResponse{
		ID:      env.ID,
		Op:      env.Op,
		Code:    env.Code,
		Msg:     env.Msg,
		Data:    env.Data,
		InTime:  env.InTime,
		OutTime: env.OutTime,
	}
	select {
	case ch <- resp:
		c.cOpReply.Inc()
	default:
		// Buffer=1; if already full — someone beat us. Mark as orphan for
		// visibility (e.g. double reply from the server).
		c.cOpOrphan.Inc()
	}
}

// handleEvent logs subscribe/unsubscribe/error events.
func (c *Conn) handleEvent(env pushEnvelope) {
	switch env.Event {
	case "subscribe":
		c.logger.Debug("ws: subscribed",
			okxlog.Str("channel", env.Arg.Channel),
			okxlog.Str("instId", env.Arg.InstID),
		)
	case "unsubscribe":
		c.logger.Debug("ws: unsubscribed",
			okxlog.Str("channel", env.Arg.Channel),
			okxlog.Str("instId", env.Arg.InstID),
		)
	case "error":
		c.logger.Warn("ws: server error",
			okxlog.Str("code", env.Code),
			okxlog.Str("msg", env.Msg),
		)
	}
}

// pingLoop sends a text "ping" every PingInterval. On error — exits
// (read-loop will also fail and supervise will reconnect).
func (c *Conn) pingLoop(ctx context.Context, socket *websocket.Conn) {
	var ticker *time.Ticker = time.NewTicker(c.cfg.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.writeMessage(socket, []byte("ping")); err != nil {
				c.cPingErr.Inc()
				c.logger.Debug("ws: ping write failed", okxlog.Err(err))
				return
			}
		}
	}
}

// sendSubscribe sends a subscribe command.
func (c *Conn) sendSubscribe(socket *websocket.Conn, sub *Subscription) error {
	var req opRequest = opRequest{
		Op: "subscribe",
		Args: []subscribeArg{{
			Channel:  sub.Channel,
			InstID:   sub.InstID,
			InstType: sub.InstType,
		}},
	}
	var raw []byte
	var err error
	raw, err = codec.Marshal(req)
	if err != nil {
		return err
	}
	return c.writeMessage(socket, raw)
}

// sendUnsubscribe sends an unsubscribe command.
func (c *Conn) sendUnsubscribe(socket *websocket.Conn, sub *Subscription) error {
	var req opRequest = opRequest{
		Op: "unsubscribe",
		Args: []subscribeArg{{
			Channel:  sub.Channel,
			InstID:   sub.InstID,
			InstType: sub.InstType,
		}},
	}
	var raw []byte
	var err error
	raw, err = codec.Marshal(req)
	if err != nil {
		return err
	}
	return c.writeMessage(socket, raw)
}

// writeMessage — thread-safe write. gorilla/websocket requires exclusive
// writes; we keep a separate writeMu.
func (c *Conn) writeMessage(socket *websocket.Conn, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = socket.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
	return socket.WriteMessage(websocket.TextMessage, data)
}

// nextBackoff doubles backoff without exceeding max.
func nextBackoff(cur, max time.Duration) time.Duration {
	cur *= 2
	if cur > max {
		cur = max
	}
	return cur
}

// applyJitter adds a random multiplier [1-j, 1+j] to d.
func applyJitter(d time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return d
	}
	var f float64 = 1.0 + (rand.Float64()*2.0-1.0)*jitter
	return time.Duration(float64(d) * f)
}

// epochSecondsString returns the current unix epoch in seconds with decimal
// digits — the format OKX expects in login-args (e.g. "1735776123.456").
func epochSecondsString() string {
	var now time.Time = time.Now()
	var sec int64 = now.Unix()
	var ms int = now.Nanosecond() / 1_000_000
	return fmt.Sprintf("%d.%03d", sec, ms)
}
