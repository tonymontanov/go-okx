/*
ФАЙЛ: examples/market-data/main.go

ОПИСАНИЕ:
Read-only public REST: спецификация инструмента, снапшот стакана, исторические
свечи. Ключи и .env НЕ требуются.

ПОКРЫТИЕ:
  - swap.MarketData().GetSymbolInfo
  - swap.MarketData().GetOrderBook (snapshot)
  - swap.MarketData().GetHistoricalCandles (1m, последние 5)

ЗАПУСК:
    go run ./examples/market-data
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	okx "github.com/tonymontanov/go-okx/v2"
	swappkg "github.com/tonymontanov/go-okx/v2/swap"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

func main() {
	var instID string = os.Getenv("OKX_INSTRUMENT")
	if instID == "" {
		instID = "BTC-USDT-SWAP"
	}

	var cfg okx.Config = okx.DefaultConfig()
	var client *okx.Client
	var err error
	client, err = okx.NewClient(cfg)
	if err != nil {
		log.Fatalf("okx.NewClient: %v", err)
	}
	defer client.Close()
	var swap *swappkg.Client = client.Swap().(*swappkg.Client)

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fmt.Printf("=== Market data for %s ===\n\n", instID)

	// 1. Спецификация инструмента.
	dumpSymbolInfo(ctx, swap, instID)

	// 2. Снапшот стакана.
	dumpOrderBook(ctx, swap, instID, 5)

	// 3. Свечи 1m, последние 5.
	dumpCandles(ctx, swap, instID, types.Timeframe1m, 5)
}

func dumpSymbolInfo(ctx context.Context, swap *swappkg.Client, instID string) {
	var info, err = swap.MarketData().GetSymbolInfo(ctx, instID)
	if err != nil {
		fmt.Printf("[symbol-info] error: %s\n\n", classify(err))
		return
	}
	fmt.Println("[symbol-info]")
	fmt.Printf("  InstID=%s SettleCcy=%s\n", info.InstID, info.SettleCcy)
	fmt.Printf("  TickSize=%s LotSize=%s MinSize=%s CtVal=%s\n\n",
		info.TickSize.String(), info.LotSize.String(), info.MinSize.String(), info.CtVal.String())
}

func dumpOrderBook(ctx context.Context, swap *swappkg.Client, instID string, depth int) {
	var ob, err = swap.MarketData().GetOrderBook(ctx, instID, depth)
	if err != nil {
		fmt.Printf("[order-book] error: %s\n\n", classify(err))
		return
	}
	fmt.Printf("[order-book depth=%d ts=%d]\n", depth, ob.Ts)
	fmt.Println("  asks (top down → close to mid):")
	var i int
	for i = len(ob.Asks) - 1; i >= 0; i-- {
		fmt.Printf("    %s × %s\n", ob.Asks[i].Price.String(), ob.Asks[i].Size.String())
	}
	if len(ob.Asks) > 0 && len(ob.Bids) > 0 {
		var spread = ob.Asks[0].Price.Sub(ob.Bids[0].Price)
		fmt.Printf("  --- spread = %s ---\n", spread.String())
	}
	fmt.Println("  bids:")
	for i = 0; i < len(ob.Bids); i++ {
		fmt.Printf("    %s × %s\n", ob.Bids[i].Price.String(), ob.Bids[i].Size.String())
	}
	fmt.Println()
}

func dumpCandles(ctx context.Context, swap *swappkg.Client, instID string, tf types.Timeframe, n int) {
	var candles, err = swap.MarketData().GetHistoricalCandles(ctx, instID, tf, n)
	if err != nil {
		fmt.Printf("[candles] error: %s\n\n", classify(err))
		return
	}
	fmt.Printf("[candles %s count=%d]\n", tf, len(candles))
	var i int
	for i = 0; i < len(candles); i++ {
		var c = candles[i]
		var t time.Time = time.UnixMilli(c.OpenTimeMs)
		fmt.Printf("  %s  O=%s  H=%s  L=%s  C=%s  V=%s  closed=%v\n",
			t.UTC().Format("2006-01-02 15:04:05"),
			c.Open.String(), c.High.String(), c.Low.String(),
			c.Close.String(), c.Volume.String(), c.Closed)
	}
	fmt.Println()
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
