/*
ФАЙЛ: examples/public-streams/main.go

ОПИСАНИЕ:
Подписывается на ВСЕ публичные WS-стримы SDK одновременно. Печатает по одному
агрегированному status-line раз в 500 ms, чтобы вывод оставался читаемым.
Ключи и .env НЕ требуются. Работает до Ctrl-C.

ПОКРЫТИЕ:
  - swap.Stream().WatchSpread       (bbo-tbt)
  - swap.Stream().WatchMarkPrice    (mark-price)
  - swap.Stream().WatchIndexPrice   (index-tickers)
  - swap.Stream().WatchLastPrice    (trades, price+ts)
  - swap.Stream().WatchAggTrades    (trades, полный AggTrade)

ЗАПУСК:
    go run ./examples/public-streams
    OKX_INSTRUMENT=ETH-USDT-SWAP go run ./examples/public-streams
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	okx "github.com/tonymontanov/go-okx/v2"
	swappkg "github.com/tonymontanov/go-okx/v2/swap"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// snapshot — атомарное состояние, обновляемое из разных каналов.
type snapshot struct {
	mu sync.RWMutex

	bestBid, bestAsk string
	mark, index      float64
	lastPrice        float64
	lastTradeTs      int64

	tradesCount atomic.Uint64
}

func main() {
	var instID string = os.Getenv("OKX_INSTRUMENT")
	if instID == "" {
		instID = "BTC-USDT-SWAP"
	}
	var indexID string = strings.TrimSuffix(instID, "-SWAP") // "BTC-USDT-SWAP" → "BTC-USDT"

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
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var s snapshot
	var streamErr = func(e error) { log.Printf("stream error: %s", classify(e)) }

	err = swap.Stream().WatchSpread(ctx, instID,
		func(u types.QuotedSpreadUpdate) {
			s.mu.Lock()
			s.bestBid = u.BestBid.String()
			s.bestAsk = u.BestAsk.String()
			s.mu.Unlock()
		}, streamErr)
	if err != nil {
		log.Fatalf("WatchSpread: %v", err)
	}

	err = swap.Stream().WatchMarkPrice(ctx, instID,
		func(price float64, _ int64) {
			s.mu.Lock()
			s.mark = price
			s.mu.Unlock()
		}, streamErr)
	if err != nil {
		log.Fatalf("WatchMarkPrice: %v", err)
	}

	err = swap.Stream().WatchIndexPrice(ctx, indexID,
		func(price float64, _ int64) {
			s.mu.Lock()
			s.index = price
			s.mu.Unlock()
		}, streamErr)
	if err != nil {
		log.Fatalf("WatchIndexPrice: %v", err)
	}

	err = swap.Stream().WatchLastPrice(ctx, instID,
		func(price float64, tsMs int64) {
			s.mu.Lock()
			s.lastPrice = price
			s.lastTradeTs = tsMs
			s.mu.Unlock()
		}, streamErr)
	if err != nil {
		log.Fatalf("WatchLastPrice: %v", err)
	}

	err = swap.Stream().WatchAggTrades(ctx, instID,
		func(t types.AggTrade) {
			s.tradesCount.Add(1)
			_ = t // вся информация уже учтена в lastPrice; здесь только счётчик
		}, streamErr)
	if err != nil {
		log.Fatalf("WatchAggTrades: %v", err)
	}

	fmt.Printf("=== Public streams: %s (index=%s) — Ctrl-C to stop ===\n\n", instID, indexID)

	var ticker *time.Ticker = time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var sigCh chan os.Signal = make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sigCh:
			fmt.Println("\nshutting down")
			return
		case <-ticker.C:
			s.mu.RLock()
			fmt.Printf("\rbid=%-10s ask=%-10s mark=%.2f index=%.2f last=%.2f trades=%d   ",
				s.bestBid, s.bestAsk, s.mark, s.index, s.lastPrice, s.tradesCount.Load())
			s.mu.RUnlock()
		}
	}
}

func classify(err error) string {
	if err == nil {
		return "<nil>"
	}
	var e *okx.Error
	if errors.As(err, &e) {
		if e.Cause != nil {
			return fmt.Sprintf("[%s code=%s] %s: %v", e.Kind, e.OKXCode, e.Message, e.Cause)
		}
		return fmt.Sprintf("[%s code=%s] %s", e.Kind, e.OKXCode, e.Message)
	}
	return err.Error()
}
