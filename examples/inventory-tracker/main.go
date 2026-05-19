/*
FILE: examples/inventory-tracker/main.go

DESCRIPTION:
WARNING: this example PLACES A REAL TRADE on the exchange. It:
  1. subscribes to private WS channels `positions` and `orders`;
  2. prints the current position state for the instrument;
  3. sends a MARKET-BUY of minimum size (1 contract by default);
  4. shows in real time how the position and orders change;
  5. after a few seconds closes the position via ClosePosition();
  6. prints the final state and exits.

Between steps 3 and 5 the user sees a "live" inventory — this is a smoke-test
of the REST trading + WS private streams integration.

CONFIG:
  - OKX_INSTRUMENT (default BTC-USDT-SWAP) — the instrument to trade.
    For low-risk tests use a cheap one, e.g.
    DOGE-USDT-SWAP (1 contract ≈ 1000 DOGE × current price / 1000 = $X).
  - OKX_SIZE (default 1) — order size in contracts. Minimum is MinSize
    of the instrument (see account-info or market-data).
  - OKX_HOLD_SECONDS (default 5) — how many seconds to hold the position open.

RUN:
    cp .env.example .env
    # fill in the keys + OKX_ALLOW_LIVE=1
    ./scripts/run.sh ./examples/inventory-tracker

COST:
    marketBUY + ClosePosition → you pay spread × 2 + commission × 2.
    For BTC-USDT-SWAP with 1 contract = 0.01 BTC at ~95k that is ~1-2 USDT
    total. Exact calculation depends on your fee tier.
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/shopspring/decimal"
	okx "github.com/tonymontanov/go-okx/v2"
	swappkg "github.com/tonymontanov/go-okx/v2/swap"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

func main() {
	// --- Config and guards ---
	var apiKey string = os.Getenv("OKX_API_KEY")
	var secret string = os.Getenv("OKX_SECRET_KEY")
	var pass string = os.Getenv("OKX_PASSPHRASE")
	if apiKey == "" || secret == "" || pass == "" {
		log.Fatal("OKX_API_KEY / OKX_SECRET_KEY / OKX_PASSPHRASE must be set")
	}
	if os.Getenv("OKX_ALLOW_LIVE") != "1" {
		log.Fatal("refusing to trade against production: set OKX_ALLOW_LIVE=1 explicitly")
	}

	var instID string = os.Getenv("OKX_INSTRUMENT")
	if instID == "" {
		instID = "BTC-USDT-SWAP"
	}
	var size decimal.Decimal = decimal.NewFromInt(1)
	if s := os.Getenv("OKX_SIZE"); s != "" {
		var err error
		size, err = decimal.NewFromString(s)
		if err != nil {
			log.Fatalf("OKX_SIZE: %v", err)
		}
	}
	var holdSeconds int64 = 5
	if s := os.Getenv("OKX_HOLD_SECONDS"); s != "" {
		var err error
		holdSeconds, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			log.Fatalf("OKX_HOLD_SECONDS: %v", err)
		}
	}

	// --- Client ---
	var cfg okx.Config = okx.DefaultConfig()
	cfg.APIKey = apiKey
	cfg.SecretKey = secret
	cfg.Passphrase = pass
	var client *okx.Client
	var err error
	client, err = okx.NewClient(cfg)
	if err != nil {
		log.Fatalf("okx.NewClient: %v", err)
	}
	defer client.Close()
	var swap *swappkg.Client = client.Swap().(*swappkg.Client)

	// ctx lives until Ctrl-C or manual cancellation at the end of the scenario.
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// SIGINT/SIGTERM handler for graceful shutdown on Ctrl-C.
	var sigCh chan os.Signal = make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("interrupt received — closing")
		cancel()
	}()

	fmt.Printf("=== Inventory tracker for %s, size=%s, hold=%ds ===\n\n",
		instID, size.String(), holdSeconds)

	// --- Initial position snapshot via REST (to see the starting state) ---
	dumpInitialState(ctx, swap, instID)

	// --- Subscribe to private streams ---
	var inv inventoryState
	subscribePositions(ctx, swap, instID, &inv)
	subscribeOrders(ctx, swap, instID, &inv)

	// Give the WS connection time to warm up and the server to send initial snapshots.
	fmt.Println("warming up streams...")
	time.Sleep(2 * time.Second)

	// --- Open position with market order ---
	fmt.Println("\n>>> placing market BUY")
	var openOrder types.OrderInfo
	openOrder, err = swap.Trading().CreateOrder(ctx, types.CreateOrderRequest{
		InstID:        instID,
		Side:          types.SideTypeBuy,
		OrderType:     types.OrderTypeMarket,
		Size:          size,
		ClientOrderID: "inv" + fmt.Sprint(time.Now().UnixMilli()),
	})
	if err != nil {
		log.Fatalf("CreateOrder(market buy): %s", classify(err))
	}
	fmt.Printf(">>> placed: ordId=%s clOrdId=%s\n", openOrder.OrderID, openOrder.ClientOrderID)

	// Hold position for N seconds — during this time WS must deliver updates.
	fmt.Printf(">>> holding for %d seconds, streaming inventory updates...\n\n", holdSeconds)
	var hold *time.Timer = time.NewTimer(time.Duration(holdSeconds) * time.Second)
	select {
	case <-hold.C:
	case <-ctx.Done():
		log.Println("ctx done during hold")
		return
	}

	// --- Close position ---
	fmt.Println("\n>>> closing position")
	err = swap.Account().ClosePosition(ctx, instID)
	if err != nil {
		log.Fatalf("ClosePosition: %s", classify(err))
	}
	fmt.Println(">>> close request accepted")

	// Give streams time to deliver the final position zero update.
	time.Sleep(3 * time.Second)

	// --- Summary ---
	fmt.Println("\n=== Final state ===")
	fmt.Printf("position updates received: %d\n", inv.posUpdates.Load())
	fmt.Printf("order updates received:    %d\n", inv.ordUpdates.Load())

	var finalPos, ferr = swap.Account().GetSymbolPosition(context.Background(), instID)
	if ferr != nil {
		fmt.Printf("final position fetch error: %s\n", classify(ferr))
		return
	}
	if finalPos.Position.IsZero() {
		fmt.Println("final position: empty (good — closed cleanly)")
	} else {
		fmt.Printf("final position: %s %s contracts (LEFTOVER — close manually!)\n",
			finalPos.PosSide, finalPos.Position.String())
	}
}

// inventoryState — counters and last known values, updated from WS.
type inventoryState struct {
	posUpdates atomic.Uint64
	ordUpdates atomic.Uint64

	mu        sync.RWMutex
	lastPos   decimal.Decimal
	lastAvgPx decimal.Decimal
}

func dumpInitialState(ctx context.Context, swap *swappkg.Client, instID string) {
	var pos, err = swap.Account().GetSymbolPosition(ctx, instID)
	if err != nil {
		fmt.Printf("[initial] position fetch error: %s\n", classify(err))
		return
	}
	if pos.Position.IsZero() {
		fmt.Printf("[initial] position is empty (PosSide=%s)\n", pos.PosSide)
	} else {
		fmt.Printf("[initial] position=%s contracts avgPx=%s uPnL=%s\n",
			pos.Position.String(), pos.AvgEntryPrice.String(), pos.UnrealizedPnL.String())
	}
}

func subscribePositions(ctx context.Context, swap *swappkg.Client, instID string, inv *inventoryState) {
	var err = swap.Stream().WatchPosition(ctx, instID, func(p types.PositionInfo) {
		inv.posUpdates.Add(1)
		inv.mu.Lock()
		inv.lastPos = p.Position
		inv.lastAvgPx = p.AvgEntryPrice
		inv.mu.Unlock()
		fmt.Printf("  [pos]  %s pos=%s avgPx=%s uPnL=%s liqPx=%s\n",
			p.PosSide, p.Position.String(), p.AvgEntryPrice.String(),
			p.UnrealizedPnL.String(), p.LiqPrice.String())
	}, func(e error) {
		log.Printf("WatchPosition error: %s", classify(e))
	})
	if err != nil {
		log.Fatalf("WatchPosition: %s", classify(err))
	}
}

func subscribeOrders(ctx context.Context, swap *swappkg.Client, instID string, inv *inventoryState) {
	var err = swap.Stream().WatchOpenOrders(ctx, instID, func(orders []types.OrderInfo) {
		inv.ordUpdates.Add(1)
		var i int
		for i = 0; i < len(orders); i++ {
			var o = orders[i]
			fmt.Printf("  [ord]  ordId=%s clOrdId=%q %s %s state=%s filled=%s/%s\n",
				o.OrderID, o.ClientOrderID, o.Side, o.OrderType, o.State,
				o.FilledSize.String(), o.Size.String())
		}
	}, func(e error) {
		log.Printf("WatchOpenOrders error: %s", classify(e))
	})
	if err != nil {
		log.Fatalf("WatchOpenOrders: %s", classify(err))
	}
}

func classify(err error) string {
	if err == nil {
		return "<nil>"
	}
	var e *okx.Error
	if errors.As(err, &e) {
		if e.Cause != nil {
			return fmt.Sprintf("[%s code=%s status=%d] %s: %v", e.Kind, e.OKXCode, e.HTTPStatus, e.Message, e.Cause)
		}
		return fmt.Sprintf("[%s code=%s status=%d] %s", e.Kind, e.OKXCode, e.HTTPStatus, e.Message)
	}
	return err.Error()
}
