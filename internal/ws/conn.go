/*
ФАЙЛ: internal/ws/conn.go

ОПИСАНИЕ:
Файл conn.go реализует управляющую обёртку над одним WebSocket-соединением
OKX. На весь Client SDK создаётся максимум два таких объекта:
  - один для PUBLIC endpoint (без login, каналы books/trades/tickers/...);
  - один для PRIVATE endpoint (с login, каналы orders/positions/account).

ОТВЕТСТВЕННОСТИ:
  - подключение / переподключение с backoff+jitter;
  - login (только private);
  - heartbeat (отправка text "ping" каждые PingInterval секунд);
  - subscribe / unsubscribe (с буферизацией: подписка регистрируется в
    registry даже если сокет временно не подключён, и применится при connect);
  - resubscribe после reconnect (полностью прозрачно для вызывающего кода);
  - dispatch входящих push-сообщений в обработчики подписок;
  - аккуратная остановка через ctx.

ВНУТРЕННЯЯ МОДЕЛЬ СОСТОЯНИЯ:
  - subs (map subscriptionKey → *Subscription) — текущая registry.
  - socket — текущий *websocket.Conn (или nil, если соединение в данный момент
    разорвано). Все RW защищены mu.
  - writeMu — отдельный мьютекс для write-операций (gorilla/websocket требует
    эксклюзивности writes).
  - runOnce / cancel — управление жизненным циклом фоновых горутин (read-loop,
    ping-loop). Перезапуск горутин происходит ВНУТРИ self-loop, наружу не
    видно.

СТРАТЕГИЯ ОШИБОК:
  - Локальные / транзиентные (ReadMessage err) — переход к reconnect.
  - Если Conn.Close() был вызван — read-loop тихо завершается.
  - Ошибки login или subscribe (event=error с code != "0") — логируются и
    учитываются в metric ws_login_failed_total / ws_subscribe_failed_total.
    Reconnect-цикл при этом продолжается (мы не «прибиваем» Conn при первой
    ошибке login, чтобы не было ситуации «сервер моргнул — клиент умер
    навсегда»).
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

// ErrConnClosed возвращается, когда SendOp вызван на уже закрытом Conn.
var ErrConnClosed = errors.New("ws: connection closed")

// ErrConnNotReady возвращается, когда SendOp вызван до установки сокета
// (Start ещё не успел подключиться или сокет в режиме reconnect-backoff).
var ErrConnNotReady = errors.New("ws: connection not ready")

// ErrOpTimeout возвращается, когда reply на op-команду не пришёл за
// отведённое время.
var ErrOpTimeout = errors.New("ws: op-request timeout")

// Subscription описывает одну подписку. Поля устанавливаются вызывающим кодом
// (swap/stream.go).
type Subscription struct {
	// Channel — имя канала OKX ("books", "bbo-tbt", "mark-price", "trades",
	// "positions", "orders", и т.п.).
	Channel string
	// InstID — инструмент. Для каналов «по инструменту» обязателен; для
	// аккаунтных (positions/orders) может быть пуст ⇒ передаётся instType.
	InstID string
	// InstType — тип инструмента ("SWAP"); используется когда InstID пуст.
	InstType string
	// Handler вызывается на каждое push-сообщение. payload — это содержимое
	// поля data (массив!). action — "snapshot" / "update" (пустое для не-books).
	Handler func(action string, payload []byte)
	// Reset вызывается перед resubscribe (после reconnect). Используется,
	// например, OrderbookEngine для сброса локального стакана перед новым
	// snapshot'ом.
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

// Config — параметры одного WS-соединения. Формируется из публичного okx.WsConfig.
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

// Conn — управляющая обёртка над одним WS-соединением OKX.
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

	// pendingMu защищает pending. Намеренно отдельный мьютекс от mu
	// (которым защищается subs/socket/closed) — это позволяет SendOp
	// регистрировать pending без блокировки read-loop'а в момент connect.
	pendingMu sync.Mutex
	pending   map[string]chan OpResponse

	// opIDSeq — atomic-счётчик для генерации correlation-id, когда
	// caller не передал свой. Стартует с 1 и инкрементируется.
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

// NewConn создаёт Conn. На этом этапе сетевая активность ещё не начинается —
// фоновые горутины запускаются при первом вызове Start или Subscribe.
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

// Start запускает фоновый supervisor-loop (connect→loop→reconnect). Возвращает
// сразу же; ошибки соединения попадают в логи и метрики. Завершается по ctx.
func (c *Conn) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		var supCtx context.Context
		supCtx, c.cancel = context.WithCancel(ctx)
		go c.supervise(supCtx)
	})
}

// Subscribe добавляет подписку в registry и, если соединение установлено,
// сразу отправляет subscribe-команду. После reconnect та же подписка будет
// автоматически восстановлена.
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
		return nil // отправим при connect
	}
	return c.sendSubscribe(socket, sub)
}

// Unsubscribe удаляет подписку из registry и (если есть сокет) шлёт unsubscribe.
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

// Close корректно завершает работу Conn (отменяет supervise-горутину и
// закрывает текущий сокет). Безопасно вызывать несколько раз.
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
SendOp отправляет op-команду (order/cancel-order/amend-order/batch-orders/
cancel-batch-orders/amend-batch-orders/mass-cancel) и ждёт reply.

ПОВЕДЕНИЕ:
  - Если req.ID пуст — генерируется уникальный возрастающий id
    (atomic-счётчик).
  - Pending-канал регистрируется ДО writeMessage, чтобы read-loop успел
    его найти даже при очень быстром ответе сервера.
  - Завершение по одному из условий: получен reply | ctx.Done | прошёл
    timeout (если timeout > 0). Во всех случаях запись удаляется из
    pending-map (defer).
  - На уже закрытом Conn возвращается ErrConnClosed.
  - Если сокет ещё не установлен (Start не вызван или backoff между
    reconnect'ами) — возвращается ErrConnNotReady. Это намеренно: op-
    команды НЕ переживают reconnect (в отличие от subscriptions), и
    «магически копить» их в буфере было бы опасно для HFT-сценариев.

ВОЗВРАЩАЕМОЕ ЗНАЧЕНИЕ:
OpResponse содержит top-level code/msg/data + inTime/outTime. Top-level
code != "0" значит, что команда отвергнута целиком (например 60012
"Illegal request"). Per-item ошибки (sCode/sMsg внутри Data) разбирает
доменный слой.
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
		// Чисто ctx-driven ожидание.
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

// failAllPending доставляет всем ожидающим SendOp синтетический OpResponse
// с заданным error-кодом, после чего очищает pending-map. Используется
// при reconnect и Close: op-команды не переживают разрыв соединения, и
// caller'ы должны получить явный ответ, а не зависнуть до таймаута.
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
		// Не блокируемся: chan buffered=1; если кто-то уже отписался —
		// просто отбрасываем.
		select {
		case ch <- OpResponse{ID: id, Code: "disconnected", Msg: msg}:
		default:
		}
	}
}

// supervise — главный supervisor-loop. Подключается → читает → при ошибке
// делает backoff и повторяет. Завершается по ctx.Done.
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

// connectAndRun устанавливает соединение, делает login (если private), шлёт
// все текущие подписки, запускает ping-loop и read-loop, ждёт их завершения.
// Возвращает ошибку, по которой supervise решит, делать ли backoff.
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
		// op-команды НЕ переживают reconnect: завершаем все pending
		// синтетическим ответом, чтобы caller'ы не висели до таймаута.
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

// performLogin шлёт login-команду и ждёт positive-event "login" с code=="0".
// Для надёжности читает несколько кадров (могут прилететь промежуточные
// pong/heartbeat), но не более ~3 секунд.
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

// readLoop читает сообщения и диспетчит их по обработчикам подписок.
// Возвращает ошибку, по которой supervise решит сделать reconnect.
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

		// op-reply имеет непустой id, эхнутый сервером из request.
		// Push-сообщения id не несут, поэтому ветви не пересекаются.
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
			// fallback: подписки на orders/positions могут приходить без instId
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

// dispatchOpReply ищет pending-канал по id и доставляет в него OpResponse.
// Если pending не найден — reply «осиротевший» (caller таймаутнул или
// отменил ctx раньше): инкрементим counter и тихо отбрасываем.
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
		// Buffer=1; если уже занят — кто-то нас опередил. Маркируем как
		// orphan для видимости (например двойной reply от сервера).
		c.cOpOrphan.Inc()
	}
}

// handleEvent логирует subscribe/unsubscribe/error события.
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

// pingLoop шлёт текстовый "ping" каждый PingInterval. При ошибке — выход
// (read-loop тоже упадёт и supervise сделает reconnect).
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

// sendSubscribe отправляет команду подписки.
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

// sendUnsubscribe отправляет команду отписки.
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

// writeMessage — потокобезопасная запись. gorilla/websocket требует
// эксклюзивности writes; держим отдельный writeMu.
func (c *Conn) writeMessage(socket *websocket.Conn, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = socket.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
	return socket.WriteMessage(websocket.TextMessage, data)
}

// nextBackoff увеличивает backoff в 2 раза, не превышая max.
func nextBackoff(cur, max time.Duration) time.Duration {
	cur *= 2
	if cur > max {
		cur = max
	}
	return cur
}

// applyJitter добавляет к d случайный множитель [1-j, 1+j].
func applyJitter(d time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return d
	}
	var f float64 = 1.0 + (rand.Float64()*2.0-1.0)*jitter
	return time.Duration(float64(d) * f)
}

// epochSecondsString возвращает текущий unix epoch в секундах с десятичными
// знаками — формат, который OKX ожидает в login-args (например "1735776123.456").
func epochSecondsString() string {
	var now time.Time = time.Now()
	var sec int64 = now.Unix()
	var ms int = now.Nanosecond() / 1_000_000
	return fmt.Sprintf("%d.%03d", sec, ms)
}
