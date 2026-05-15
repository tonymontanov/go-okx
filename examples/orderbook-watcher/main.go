/*
ФАЙЛ: examples/orderbook-watcher/main.go

ОПИСАНИЕ:
Минимальный пример: подписываемся на public канал "books" OKX по BTC-USDT-SWAP,
поддерживаем локальный стакан через orderbook.Engine с CRC32-валидацией
(встроено в Stream().WatchOrderbook) и печатаем верхние bid/ask каждые 500 ms.

ЗАПУСК:
    go run ./examples/orderbook-watcher

Ключи не нужны — канал публичный.
*/

package main

import (
	"context"
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

	var ticks atomic.Uint64
	var bestBid atomic.Value
	var bestAsk atomic.Value

	err = swap.Stream().WatchOrderbook(ctx, "BTC-USDT-SWAP", 5,
		func(snap types.OrderBookSnapshot) {
			ticks.Add(1)
			if len(snap.Bids) > 0 {
				bestBid.Store(snap.Bids[0].Price.String())
			}
			if len(snap.Asks) > 0 {
				bestAsk.Store(snap.Asks[0].Price.String())
			}
		},
		func(streamErr error) {
			log.Printf("stream error: %v", streamErr)
		},
	)
	if err != nil {
		log.Fatalf("WatchOrderbook: %v", err)
	}

	var printer *time.Ticker = time.NewTicker(500 * time.Millisecond)
	defer printer.Stop()

	var sigCh chan os.Signal = make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sigCh:
			fmt.Println("\nshutting down")
			return
		case <-printer.C:
			var bid, ask any
			bid = bestBid.Load()
			ask = bestAsk.Load()
			fmt.Printf("ticks=%d  bid=%v  ask=%v\n", ticks.Load(), bid, ask)
		}
	}
}
