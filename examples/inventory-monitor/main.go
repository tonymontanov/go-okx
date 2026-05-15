/*
ФАЙЛ: examples/inventory-monitor/main.go

ОПИСАНИЕ:
Бесконечный мониторинг инвентаря (позиция + открытые ордера) через приватные
WS-каналы OKX. НЕ совершает сделок — только подписывается и печатает.

В отличие от inventory-tracker, этот пример НЕ завершается сам: он живёт до
Ctrl-C. Любые изменения позиции — в т. ч. переход через ноль, открытие в
противоположную сторону, частичные заполнения — будут отображаться в реальном
времени.

Это правильная заготовка для алгоритма, который должен непрерывно следить за
своим состоянием на бирже.

ПОКРЫТИЕ:
  - swap.Stream().WatchPosition       (приватный канал positions)
  - swap.Stream().WatchOpenOrders     (приватный канал orders)
  - первичный snapshot позиции через swap.Account().GetSymbolPosition

ЗАПУСК:
    ./scripts/run.sh ./examples/inventory-monitor
    OKX_INSTRUMENT=ETH-USDT-SWAP ./scripts/run.sh ./examples/inventory-monitor

Чтобы убедиться, что SDK не завершается при pos=0:
  1. запусти этот пример;
  2. в OKX-приложении вручную открой и сразу закрой маленькую позицию;
  3. в консоли увидишь обновления [pos]/[ord] и при открытии, и при закрытии,
     и значение pos=0 — программа НЕ выйдет.
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

	// ctx живёт до Ctrl-C. Поскольку мы НЕ ставим тут никаких таймеров и не
	// зовём cancel() в нормальном потоке — все Watch* живут столько же.
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("=== Inventory monitor for %s — Ctrl-C to stop ===\n\n", instID)

	// 1. Стартовый snapshot через REST (чтобы видеть состояние до первого WS-апдейта).
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

	// 2. Счётчики апдейтов — для итоговой статистики при выходе.
	var posUpdates, ordUpdates atomic.Uint64

	// 3. Подписываемся на изменения позиции.
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

	// 4. Подписываемся на изменения ордеров.
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

	// 5. Ждём Ctrl-C. ctx остаётся живым — Watch* продолжают работать.
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
