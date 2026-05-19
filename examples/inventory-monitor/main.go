/*
FILE: examples/inventory-monitor/main.go

DESCRIPTION:
Infinite inventory monitoring (position + open orders) via private OKX WS
channels. Does NOT place trades — subscribes and prints only.

Unlike inventory-tracker, this example does NOT exit on its own: it runs until
Ctrl-C. Any position changes — including crossing zero, opening in the opposite
direction, partial fills — are displayed in real time.

This is a correct template for an algorithm that must continuously track its
exchange state.

COVERAGE:
  - swap.Stream().WatchPosition       (private positions channel)
  - swap.Stream().WatchOpenOrders     (private orders channel)
  - initial position snapshot via swap.Account().GetSymbolPosition

RUN:
    ./scripts/run.sh ./examples/inventory-monitor
    OKX_INSTRUMENT=ETH-USDT-SWAP ./scripts/run.sh ./examples/inventory-monitor

To verify the SDK does not exit when pos=0:
  1. run this example;
  2. manually open and immediately close a small position in the OKX app;
  3. the console will show [pos]/[ord] updates on open, on close, and pos=0 —
     the program will NOT exit.
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	okx "github.com/tonymontanov/go-okx/v2"
	swappkg "github.com/tonymontanov/go-okx/v2/swap"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

func main() {
	var apiKey string = os.Getenv("OKX_API_KEY")
	var secret string = os.Getenv("OKX_SECRET_KEY")
	var pass string = os.Getenv("OKX_PASSPHRASE")
	if apiKey == "" || secret == "" || pass == "" {
		log.Fatal("OKX_API_KEY / OKX_SECRET_KEY / OKX_PASSPHRASE must be set")
	}

	var instID string = os.Getenv("OKX_INSTRUMENT")
	if instID == "" {
		instID = "BTC-USDT-SWAP"
	}

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

	// ctx lives until Ctrl-C. Since we do NOT set any timers and do not call
	// cancel() in the normal flow — all Watch* live as long.
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("=== Inventory monitor for %s — Ctrl-C to stop ===\n\n", instID)

	// 1. Initial snapshot via REST (to see the state before the first WS update).
	var pos, perr = swap.Account().GetSymbolPosition(ctx, instID)
	if perr != nil {
		fmt.Printf("[initial] position fetch error: %s\n", classify(perr))
	} else if pos.Position.IsZero() {
		fmt.Printf("[initial] position is empty (PosSide=%s)\n", pos.PosSide)
	} else {
		fmt.Printf("[initial] pos=%s avgPx=%s uPnL=%s\n",
			pos.Position.String(), pos.AvgEntryPrice.String(), pos.UnrealizedPnL.String())
	}
	fmt.Println()

	// 2. Update counters — for summary statistics on exit.
	var posUpdates, ordUpdates atomic.Uint64

	// 3. Subscribe to position changes.
	err = swap.Stream().WatchPosition(ctx, instID, func(p types.PositionInfo) {
		posUpdates.Add(1)
		fmt.Printf("%s  [pos]  %s pos=%s avgPx=%s uPnL=%s\n",
			time.Now().Format("15:04:05.000"),
			p.PosSide, p.Position.String(),
			p.AvgEntryPrice.String(), p.UnrealizedPnL.String())
	}, func(e error) {
		log.Printf("WatchPosition error: %s", classify(e))
	})
	if err != nil {
		log.Fatalf("WatchPosition: %s", classify(err))
	}

	// 4. Subscribe to order changes.
	err = swap.Stream().WatchOpenOrders(ctx, instID, func(orders []types.OrderInfo) {
		ordUpdates.Add(1)
		var i int
		for i = 0; i < len(orders); i++ {
			var o = orders[i]
			fmt.Printf("%s  [ord]  ordId=%s clOrdId=%q %s %s state=%s filled=%s/%s\n",
				time.Now().Format("15:04:05.000"),
				o.OrderID, o.ClientOrderID, o.Side, o.OrderType, o.State,
				o.FilledSize.String(), o.Size.String())
		}
	}, func(e error) {
		log.Printf("WatchOpenOrders error: %s", classify(e))
	})
	if err != nil {
		log.Fatalf("WatchOpenOrders: %s", classify(err))
	}

	fmt.Println("monitoring... (Ctrl-C to stop)")
	fmt.Println()

	// 5. Wait for Ctrl-C. ctx stays alive — Watch* keep running.
	var sigCh chan os.Signal = make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n=== Shutting down ===")
	fmt.Printf("position updates: %d\n", posUpdates.Load())
	fmt.Printf("order updates:    %d\n", ordUpdates.Load())
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
