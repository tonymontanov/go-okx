/*
ФАЙЛ: examples/inventory-tracker/main.go

ОПИСАНИЕ:
ВНИМАНИЕ: этот пример СОВЕРШАЕТ РЕАЛЬНУЮ СДЕЛКУ на бирже. Он:
  1. подписывается на private WS-каналы `positions` и `orders`;
  2. печатает текущее состояние позиции по инструменту;
  3. отправляет МАРКЕТ-BUY минимального размера (1 контракт по умолчанию);
  4. в реальном времени показывает, как меняются позиция и ордера;
  5. через несколько секунд закрывает позицию через ClosePosition();
  6. печатает финальное состояние и завершается.

Между шагами 3 и 5 пользователь видит «живой» инвентарь — это smoke-test
связки REST trading + WS private streams.

КОНФИГ:
  - OKX_INSTRUMENT (default BTC-USDT-SWAP) — какой инструмент торгуем.
    Для тестов с минимальным риском возьмите дешёвый: например
    DOGE-USDT-SWAP (1 contract ≈ 1000 DOGE × текущая цена / 1000 = $X).
  - OKX_SIZE (default 1) — размер ордера в контрактах. Минимум — MinSize
    инструмента (см. account-info или market-data).
  - OKX_HOLD_SECONDS (default 5) — сколько секунд держать позицию открытой.

ЗАПУСК:
    cp .env.example .env
    # заполнить ключи + OKX_ALLOW_LIVE=1
    ./scripts/run.sh ./examples/inventory-tracker

СТОИМОСТЬ:
    Сделка marketBUY + ClosePosition → платите спред × 2 + комиссию × 2.
    Для BTC-USDT-SWAP с 1 контракт = 0.01 BTC при цене ~95k это ~1-2 USDT
    суммарно. Точный расчёт зависит от твоего fee tier.
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
	// --- Конфиг и предохранители ---
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

	// --- Клиент ---
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

	// ctx живёт до Ctrl-C или ручной отмены в конце сценария.
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Обработчик SIGINT/SIGTERM, чтобы аккуратно закрыться при Ctrl-C.
	var sigCh chan os.Signal = make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("interrupt received — closing")
		cancel()
	}()

	fmt.Printf("=== Inventory tracker for %s, size=%s, hold=%ds ===\n\n",
		instID, size.String(), holdSeconds)

	// --- Предварительный snapshot позиции через REST (чтобы увидеть стартовое состояние) ---
	dumpInitialState(ctx, swap, instID)

	// --- Подписки на private streams ---
	var inv inventoryState
	subscribePositions(ctx, swap, instID, &inv)
	subscribeOrders(ctx, swap, instID, &inv)

	// Дадим WS-конекту прогреться и серверу прислать первые snapshots.
	fmt.Println("warming up streams...")
	time.Sleep(2 * time.Second)

	// --- Открываем позицию маркетом ---
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

	// Держим позицию N секунд — за это время WS обязан прислать обновления.
	fmt.Printf(">>> holding for %d seconds, streaming inventory updates...\n\n", holdSeconds)
	var hold *time.Timer = time.NewTimer(time.Duration(holdSeconds) * time.Second)
	select {
	case <-hold.C:
	case <-ctx.Done():
		log.Println("ctx done during hold")
		return
	}

	// --- Закрываем позицию ---
	fmt.Println("\n>>> closing position")
	err = swap.Account().ClosePosition(ctx, instID)
	if err != nil {
		log.Fatalf("ClosePosition: %s", classify(err))
	}
	fmt.Println(">>> close request accepted")

	// Дадим стримам прислать финальное обнуление позиции.
	time.Sleep(3 * time.Second)

	// --- Сводка ---
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

// inventoryState — счётчики и последние известные значения, обновляются из WS.
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
