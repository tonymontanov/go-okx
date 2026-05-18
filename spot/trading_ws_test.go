/*
ФАЙЛ: spot/trading_ws_test.go

ОПИСАНИЕ:
Contract-тесты WSTradingClient для SPOT. Поднимают локальный mock WS-
сервер (gorilla upgrade), проверяют:

  - happy-path CreateOrder/ModifyOrder/CancelOrder;
  - per-item reject через sCode != "0" → типизированная *okx.Error;
  - batch CreateBatchOrders с частичным reject (агрегация ошибок);
  - корректный correlation id (id в request == id в reply).

Mock-сервер не валидирует sign — это не задача этого слоя (покрыто
internal/ws). Любой login принимается; на op-команды отдаются заранее
заготовленные ответы по типу op-имени.
*/

package spot

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
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

// wsOpRequest — упрощённая копия protocolа OKX WS (id+op+args) для парсинга
// в mock-сервере. Соответствует internal/ws.opRequestMessage.
type wsOpRequest struct {
	ID   string `json:"id"`
	Op   string `json:"op"`
	Args []any  `json:"args"`
}

// mockWSScript — поведение mock WS-сервера на op-команды.
type mockWSScript struct {
	// onOp принимает (id, op, args-as-raw-json) и возвращает body reply
	// (без поля id — оно подставится автоматически).
	onOp func(id, op, rawArgs string) string
}

// startMockWS поднимает локальный WS-сервер. Возвращает ws://-URL.
func startMockWS(t *testing.T, script mockWSScript) (string, *httptest.Server) {
	t.Helper()
	var upgrader websocket.Upgrader = websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
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
			// detect login
			if strings.Contains(s, `"op":"login"`) {
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"event":"login","code":"0","msg":""}`))
				continue
			}
			// detect op with id
			var req wsOpRequest
			if err = codec.Unmarshal(message, &req); err == nil && req.ID != "" && req.Op != "" {
				if script.onOp == nil {
					continue
				}
				var argsBytes []byte
				argsBytes, _ = codec.Marshal(req.Args)
				var reply string = script.onOp(req.ID, req.Op, string(argsBytes))
				if reply == "" {
					continue
				}
				_ = c.WriteMessage(websocket.TextMessage, []byte(reply))
			}
		}
	}))
	var u string = strings.Replace(srv.URL, "http://", "ws://", 1)
	return u, srv
}

// newSpotWithMockWS создаёт spot.Client, чей private WS указывает на
// mock-сервер. REST в этих тестах не используется.
func newSpotWithMockWS(t *testing.T, wsURL string) *Client {
	t.Helper()
	var cfg okx.Config = okx.DefaultConfig()
	cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "k", "s", "p"
	cfg.WS.PrivateURL = wsURL
	cfg.WS.PublicURL = wsURL // не используется в этих тестах, но не должно быть пустым
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
	t.Cleanup(func() { _ = oc.Close() })
	return oc.Spot().(*Client)
}

func TestWS_CreateOrder_HappyPath(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, _ string) string {
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"abc-1","clOrdId":"cli-1","sCode":"0"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSpotWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var info types.OrderInfo
	var err error
	info, err = sc.Trading().WS().CreateOrder(ctx, types.CreateOrderRequest{
		InstID:        "BTC-USDT",
		Side:          types.SideTypeBuy,
		OrderType:     types.OrderTypeLimit,
		Price:         mustDec("60000"),
		Size:          mustDec("0.001"),
		ClientOrderID: "cli1",
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if info.OrderID != "abc-1" || info.ClientOrderID != "cli-1" {
		t.Fatalf("ids mismatch: %+v", info)
	}
	if info.State != types.OrderStateLive {
		t.Fatalf("State must be Live, got %q", info.State)
	}
}

func TestWS_CreateOrder_RejectByExchange(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, _ string) string {
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"","clOrdId":"cli-1","sCode":"51020","sMsg":"size too small"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSpotWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var _, err = sc.Trading().WS().CreateOrder(ctx, types.CreateOrderRequest{
		InstID:    "BTC-USDT",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeLimit,
		Price:     mustDec("60000"),
		Size:      mustDec("0.001"),
	})
	if err == nil {
		t.Fatalf("expected exchange error")
	}
	var oerr *okx.Error
	if !asOkxError(err, &oerr) {
		t.Fatalf("expected *okx.Error, got %T: %v", err, err)
	}
	if oerr.OKXCode != "51020" {
		t.Fatalf("expected OKXCode=51020, got %q", oerr.OKXCode)
	}
}

func TestWS_CancelOrder_HappyPath(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, _ string) string {
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"abc-1","clOrdId":"","sCode":"0"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSpotWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error = sc.Trading().WS().CancelOrder(ctx, types.CancelOrderRequest{
		InstID:  "BTC-USDT",
		OrderID: "abc-1",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
}

func TestWS_ModifyOrder_HappyPath(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, _ string) string {
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"abc-1","clOrdId":"","sCode":"0"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSpotWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var info types.OrderInfo
	var err error
	info, err = sc.Trading().WS().ModifyOrder(ctx, types.ModifyOrderRequest{
		InstID:   "BTC-USDT",
		OrderID:  "abc-1",
		NewPrice: mustDec("60100"),
	})
	if err != nil {
		t.Fatalf("ModifyOrder: %v", err)
	}
	if info.OrderID != "abc-1" {
		t.Fatalf("ids mismatch: %+v", info)
	}
}

func TestWS_CreateBatchOrders_PartialReject(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, _ string) string {
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[
				{"ordId":"a-1","clOrdId":"c1","sCode":"0"},
				{"ordId":"","clOrdId":"c2","sCode":"51020","sMsg":"size too small"}
			]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSpotWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var reqs []types.CreateOrderRequest = []types.CreateOrderRequest{
		{InstID: "BTC-USDT", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("60000"), Size: mustDec("0.001"), ClientOrderID: "c1"},
		{InstID: "BTC-USDT", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("60000"), Size: mustDec("0.001"), ClientOrderID: "c2"},
	}
	var infos []types.OrderInfo
	var err error
	infos, err = sc.Trading().WS().CreateBatchOrders(ctx, reqs)
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}
	if infos[0].State != types.OrderStateLive || infos[0].OrderID != "a-1" {
		t.Fatalf("first item must be Live with OrderID=a-1, got %+v", infos[0])
	}
	if infos[1].State != types.OrderStateUnknown {
		t.Fatalf("second item must be Unknown, got %+v", infos[1])
	}
	if err == nil {
		t.Fatalf("expected aggregated error")
	}
}

func TestWS_CorrelationID(t *testing.T) {
	// Сервер задерживает ответ на 50мс — за это время мы успеваем
	// отправить второй запрос. Проверяем, что каждый caller получает
	// свой reply (id matched).
	var got1 atomic.Int64
	var got2 atomic.Int64
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, args string) string {
			// echo id в clOrdId
			time.Sleep(20 * time.Millisecond)
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"o","clOrdId":"` + id + `","sCode":"0"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSpotWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var doneCh chan struct{} = make(chan struct{}, 2)
	go func() {
		var info, _ = sc.Trading().WS().CreateOrder(ctx, types.CreateOrderRequest{
			InstID: "BTC-USDT", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit,
			Price: mustDec("60000"), Size: mustDec("0.001"),
		})
		if info.ClientOrderID != "" {
			got1.Store(1)
		}
		doneCh <- struct{}{}
	}()
	go func() {
		var info, _ = sc.Trading().WS().CreateOrder(ctx, types.CreateOrderRequest{
			InstID: "BTC-USDT", Side: types.SideTypeSell, OrderType: types.OrderTypeLimit,
			Price: mustDec("60000"), Size: mustDec("0.001"),
		})
		if info.ClientOrderID != "" {
			got2.Store(1)
		}
		doneCh <- struct{}{}
	}()
	<-doneCh
	<-doneCh
	if got1.Load() != 1 || got2.Load() != 1 {
		t.Fatalf("both correlated replies expected, got got1=%d got2=%d", got1.Load(), got2.Load())
	}
}

func TestWS_CancelAllAfter_HappyPath(t *testing.T) {
	var seenArgs string
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, args string) string {
			if op != "cancel-all-after" {
				return `{"id":"` + id + `","op":"` + op + `","code":"60012","msg":"unexpected op"}`
			}
			seenArgs = args
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"triggerTime":"1700000010000","ts":"1700000000000"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSpotWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var res types.CancelAllAfterResult
	var err error
	res, err = sc.Trading().WS().CancelAllAfter(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("CancelAllAfter: %v", err)
	}
	if res.TriggerTimeMs != 1700000010000 || res.TsMs != 1700000000000 {
		t.Fatalf("unexpected response: %+v", res)
	}
	if !strings.Contains(seenArgs, `"timeOut":"30"`) {
		t.Fatalf("expected timeOut=30 in args, got %q", seenArgs)
	}
}

// asOkxError — local helper для безопасного type-assert через errors.As.
func asOkxError(err error, target **okx.Error) bool {
	var oerr *okx.Error
	for cur := err; cur != nil; {
		if o, ok := cur.(*okx.Error); ok {
			oerr = o
			break
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := cur.(unwrapper); ok {
			cur = u.Unwrap()
			continue
		}
		break
	}
	if oerr == nil {
		return false
	}
	*target = oerr
	return true
}
