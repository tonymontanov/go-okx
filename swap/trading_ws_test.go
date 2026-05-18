/*
ФАЙЛ: swap/trading_ws_test.go

ОПИСАНИЕ:
Contract-тесты WSTradingClient для SWAP. Зеркальные тесты для spot/
trading_ws_test.go, плюс отдельная проверка MassCancel (применим
полноценно на SWAP с реальным instFamily).
*/

package swap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

type wsOpRequest struct {
	ID   string `json:"id"`
	Op   string `json:"op"`
	Args []any  `json:"args"`
}

type mockWSScript struct {
	onOp func(id, op, rawArgs string) string
}

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
			if strings.Contains(s, `"op":"login"`) {
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"event":"login","code":"0","msg":""}`))
				continue
			}
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

func newSwapWithMockWS(t *testing.T, wsURL string) *Client {
	t.Helper()
	var cfg okx.Config = okx.DefaultConfig()
	cfg.APIKey, cfg.SecretKey, cfg.Passphrase = "k", "s", "p"
	cfg.WS.PrivateURL = wsURL
	cfg.WS.PublicURL = wsURL
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
	return oc.Swap().(*Client)
}

func TestWS_CreateOrder_HappyPath(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, _ string) string {
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"o-1","clOrdId":"c-1","sCode":"0"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSwapWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var info types.OrderInfo
	var err error
	info, err = sc.Trading().WS().CreateOrder(ctx, types.CreateOrderRequest{
		InstID:    "BTC-USDT-SWAP",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeLimit,
		Price:     mustDec("60000"),
		Size:      mustDec("1"),
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if info.OrderID != "o-1" || info.ClientOrderID != "c-1" {
		t.Fatalf("ids mismatch: %+v", info)
	}
}

func TestWS_CancelOrder_HappyPath(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, _ string) string {
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"o-1","clOrdId":"","sCode":"0"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSwapWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error = sc.Trading().WS().CancelOrder(ctx, types.CancelOrderRequest{
		InstID:  "BTC-USDT-SWAP",
		OrderID: "o-1",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
}

func TestWS_MassCancel_HappyPath(t *testing.T) {
	// Сервер проверяет, что прилетел именно op="mass-cancel" с
	// instType+instFamily в args.
	var seenArgs string
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, args string) string {
			if op != "mass-cancel" {
				return `{"id":"` + id + `","op":"` + op + `","code":"60012","msg":"unexpected op"}`
			}
			seenArgs = args
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[{"ordId":"","clOrdId":"","sCode":"0"}]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSwapWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error = sc.Trading().WS().MassCancel(ctx, "SWAP", "BTC-USDT")
	if err != nil {
		t.Fatalf("MassCancel: %v", err)
	}
	if !strings.Contains(seenArgs, `"instType":"SWAP"`) {
		t.Fatalf("expected instType=SWAP in args, got %q", seenArgs)
	}
	if !strings.Contains(seenArgs, `"instFamily":"BTC-USDT"`) {
		t.Fatalf("expected instFamily=BTC-USDT in args, got %q", seenArgs)
	}
}

func TestWS_CreateBatchOrders_PartialReject(t *testing.T) {
	var url string
	var srv *httptest.Server
	url, srv = startMockWS(t, mockWSScript{
		onOp: func(id, op, _ string) string {
			return `{"id":"` + id + `","op":"` + op + `","code":"0","msg":"","data":[
				{"ordId":"o-1","clOrdId":"c1","sCode":"0"},
				{"ordId":"","clOrdId":"c2","sCode":"51020","sMsg":"size too small"}
			]}`
		},
	})
	defer srv.Close()

	var sc *Client = newSwapWithMockWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var reqs []types.CreateOrderRequest = []types.CreateOrderRequest{
		{InstID: "BTC-USDT-SWAP", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("60000"), Size: mustDec("1"), ClientOrderID: "c1"},
		{InstID: "BTC-USDT-SWAP", Side: types.SideTypeBuy, OrderType: types.OrderTypeLimit, Price: mustDec("60000"), Size: mustDec("1"), ClientOrderID: "c2"},
	}
	var infos []types.OrderInfo
	var err error
	infos, err = sc.Trading().WS().CreateBatchOrders(ctx, reqs)
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}
	if infos[0].State != types.OrderStateLive || infos[0].OrderID != "o-1" {
		t.Fatalf("first item must be Live, got %+v", infos[0])
	}
	if infos[1].State != types.OrderStateUnknown {
		t.Fatalf("second item must be Unknown, got %+v", infos[1])
	}
	if err == nil {
		t.Fatalf("expected aggregated error")
	}
}
