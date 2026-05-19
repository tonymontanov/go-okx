/*
FILE: spot/stream_books_test.go

DESCRIPTION:
Smoke tests for the new book channels (Phase 2.3):
  - WatchOrderbookBooks5 subscribes to the "books5" channel specifically and
    does not use the engine (snapshot-only);
  - WatchOrderbookL2Tbt / WatchOrderbookBooks50L2Tbt subscribe to the correct
    channels (l2-tbt / books50-l2-tbt) and use the same engine as regular "books".

The mock server is minimal: accepts subscribe, responds with ack, sends one
hard-coded data push.
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

// subProbe — minimal subscribe-command structure for parsing.
type subProbe struct {
	Op   string `json:"op"`
	Args []struct {
		Channel string `json:"channel"`
		InstID  string `json:"instId"`
	} `json:"args"`
}

// startBookMockWS starts a mock server: responds to any subscribe with
// ack + one push containing pre-baked bids/asks. seenChannel stores the
// name of the channel that actually received the subscription — to verify
// the SDK sends books5/books-l2-tbt/etc rather than "books".
func startBookMockWS(t *testing.T, push string) (url string, srv *httptest.Server, seenChannel *atomic.Pointer[string]) {
	t.Helper()
	var upgrader websocket.Upgrader = websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	seenChannel = &atomic.Pointer[string]{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			var sp subProbe
			if err = codec.Unmarshal(message, &sp); err == nil && sp.Op == "subscribe" && len(sp.Args) > 0 {
				var ch string = sp.Args[0].Channel
				var inst string = sp.Args[0].InstID
				seenChannel.Store(&ch)
				var ack string = `{"event":"subscribe","arg":{"channel":"` + ch + `","instId":"` + inst + `"}}`
				_ = c.WriteMessage(websocket.TextMessage, []byte(ack))
				// build push, substituting channel/instId
				var enriched string = strings.ReplaceAll(push, "{CHANNEL}", ch)
				enriched = strings.ReplaceAll(enriched, "{INSTID}", inst)
				_ = c.WriteMessage(websocket.TextMessage, []byte(enriched))
			}
		}
	}))
	url = strings.Replace(srv.URL, "http://", "ws://", 1)
	return
}

// newSpotWithPublicWS creates a spot.Client with public WS pointing to the
// mock server. Subscriptions go through the public conn (no login), so a
// signer is not required, but we stub it anyway — otherwise DefaultConfig
// may fail validation.
func newSpotWithPublicWS(t *testing.T, wsURL string) *Client {
	t.Helper()
	var cfg okx.Config = okx.DefaultConfig()
	cfg.WS.PublicURL = wsURL
	cfg.WS.PrivateURL = wsURL
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

func TestStream_WatchOrderbookBooks5(t *testing.T) {
	// snapshot-only push: one snapshot, no action="snapshot"/"update"
	var push string = `{"arg":{"channel":"{CHANNEL}","instId":"{INSTID}"},"data":[{"asks":[["60010","1"],["60011","2"]],"bids":[["60000","3"],["59999","4"]],"ts":"1700000000000"}]}`
	var url string
	var srv *httptest.Server
	var seen *atomic.Pointer[string]
	url, srv, seen = startBookMockWS(t, push)
	defer srv.Close()

	var sc *Client = newSpotWithPublicWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var gotMu sync.Mutex
	var got types.OrderBookSnapshot
	var gotCh chan struct{} = make(chan struct{}, 1)
	var err error = sc.Stream().WatchOrderbookBooks5(ctx, "BTC-USDT", func(s types.OrderBookSnapshot) {
		gotMu.Lock()
		got = s
		gotMu.Unlock()
		select {
		case gotCh <- struct{}{}:
		default:
		}
	}, nil)
	if err != nil {
		t.Fatalf("WatchOrderbookBooks5: %v", err)
	}

	select {
	case <-gotCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("no snapshot received")
	}

	if ch := seen.Load(); ch == nil || *ch != "books5" {
		var got string
		if ch != nil {
			got = *ch
		}
		t.Fatalf("expected subscribe to books5, got %q", got)
	}
	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got.Bids) != 2 || len(got.Asks) != 2 {
		t.Fatalf("expected 2+2 levels, got %d+%d", len(got.Bids), len(got.Asks))
	}
	if !got.Bids[0].Price.Equal(mustDec("60000")) {
		t.Fatalf("best bid: %v", got.Bids[0].Price)
	}
}

func TestStream_WatchOrderbookL2Tbt_SubscribesToCorrectChannel(t *testing.T) {
	// Ready snapshot, like a regular "books" push (action+seqId+checksum).
	var push string = `{"arg":{"channel":"{CHANNEL}","instId":"{INSTID}"},"action":"snapshot","data":[{"asks":[["60010","1"]],"bids":[["60000","1"]],"ts":"1700000000000","checksum":0,"seqId":1,"prevSeqId":-1}]}`
	var url string
	var srv *httptest.Server
	var seen *atomic.Pointer[string]
	url, srv, seen = startBookMockWS(t, push)
	defer srv.Close()

	var sc *Client = newSpotWithPublicWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got atomic.Int64
	_ = sc.Stream().WatchOrderbookL2Tbt(ctx, "BTC-USDT", 5, func(_ types.OrderBookSnapshot) {
		got.Add(1)
	}, nil)

	// wait until the push arrives and the engine processes it
	var deadline time.Time = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ch := seen.Load(); ch == nil || *ch != "books-l2-tbt" {
		var name string
		if ch != nil {
			name = *ch
		}
		t.Fatalf("expected subscribe to books-l2-tbt, got %q", name)
	}
	if got.Load() == 0 {
		t.Fatalf("handler must be invoked at least once")
	}
}

func TestStream_WatchOrderbookBooks50L2Tbt_SubscribesToCorrectChannel(t *testing.T) {
	var push string = `{"arg":{"channel":"{CHANNEL}","instId":"{INSTID}"},"action":"snapshot","data":[{"asks":[["60010","1"]],"bids":[["60000","1"]],"ts":"1700000000000","checksum":0,"seqId":1,"prevSeqId":-1}]}`
	var url string
	var srv *httptest.Server
	var seen *atomic.Pointer[string]
	url, srv, seen = startBookMockWS(t, push)
	defer srv.Close()

	var sc *Client = newSpotWithPublicWS(t, url)
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got atomic.Int64
	_ = sc.Stream().WatchOrderbookBooks50L2Tbt(ctx, "BTC-USDT", 5, func(_ types.OrderBookSnapshot) {
		got.Add(1)
	}, nil)

	var deadline time.Time = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ch := seen.Load(); ch == nil || *ch != "books50-l2-tbt" {
		var name string
		if ch != nil {
			name = *ch
		}
		t.Fatalf("expected subscribe to books50-l2-tbt, got %q", name)
	}
}
