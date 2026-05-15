/*
ФАЙЛ: examples/account-info/main.go

ОПИСАНИЕ:
Read-only smoke-test приватных REST-эндпоинтов. НЕ ставит ордера, НЕ изменяет
состояние аккаунта. Полезен для первой проверки, что ключи валидны и SDK
правильно подписывает приватные запросы.

ПОКРЫТИЕ:
  - swap.Account().GetSymbolPosition(instId)
  - swap.Account().GetPositions(instId)
  - swap.Account().GetOpenOrders(instId)
  - swap.MarketData().GetSymbolInfo(instId)  (public, для подсветки спецификации)

ЗАПУСК:
    ./scripts/run.sh ./examples/account-info
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

	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fmt.Printf("=== Account info for %s ===\n\n", instID)

	// 1. Symbol info — публичный вызов, без подписи.
	dumpSymbolInfo(ctx, swap, instID)

	// 2. Position.
	dumpPosition(ctx, swap, instID)

	// 3. Открытые ордера.
	dumpOpenOrders(ctx, swap, instID)
}

func dumpSymbolInfo(ctx context.Context, swap *swappkg.Client, instID string) {
	var info, err = swap.MarketData().GetSymbolInfo(ctx, instID)
	if err != nil {
		fmt.Printf("[symbol-info] error: %s\n\n", classify(err))
		return
	}
	fmt.Println("[symbol-info]")
	fmt.Printf("  InstID         = %s\n", info.InstID)
	fmt.Printf("  SettleCcy      = %s\n", info.SettleCcy)
	fmt.Printf("  CtVal          = %s\n", info.CtVal.String())
	fmt.Printf("  TickSize       = %s (precision=%d)\n", info.TickSize.String(), info.PricePrecision)
	fmt.Printf("  LotSize        = %s (precision=%d)\n", info.LotSize.String(), info.QuantityPrecision)
	fmt.Printf("  MinSize        = %s contracts\n", info.MinSize.String())
	fmt.Printf("  MaxLimitSize   = %s\n", info.MaxLimitSize.String())
	fmt.Printf("  MaxMarketSize  = %s\n\n", info.MaxMarketSize.String())
}

func dumpPosition(ctx context.Context, swap *swappkg.Client, instID string) {
	var pos, err = swap.Account().GetSymbolPosition(ctx, instID)
	if err != nil {
		fmt.Printf("[position] error: %s\n\n", classify(err))
		return
	}
	fmt.Println("[position]")
	if pos.Position.IsZero() {
		fmt.Printf("  empty (PosSide=%s)\n\n", pos.PosSide)
		return
	}
	fmt.Printf("  PosSide        = %s\n", pos.PosSide)
	fmt.Printf("  Position       = %s contracts\n", pos.Position.String())
	fmt.Printf("  AvgEntryPrice  = %s\n", pos.AvgEntryPrice.String())
	fmt.Printf("  UnrealizedPnL  = %s\n", pos.UnrealizedPnL.String())
	fmt.Printf("  LiqPrice       = %s\n", pos.LiqPrice.String())
	fmt.Printf("  UpdatedAtMs    = %d\n\n", pos.UpdatedAtMs)
}

func dumpOpenOrders(ctx context.Context, swap *swappkg.Client, instID string) {
	var orders, err = swap.Account().GetOpenOrders(ctx, instID)
	if err != nil {
		fmt.Printf("[open-orders] error: %s\n\n", classify(err))
		return
	}
	fmt.Println("[open-orders]")
	if len(orders) == 0 {
		fmt.Println("  none")
		fmt.Println()
		return
	}
	var i int
	for i = 0; i < len(orders); i++ {
		var o = orders[i]
		fmt.Printf("  #%d ordId=%s clOrdId=%q side=%s type=%s state=%s\n",
			i, o.OrderID, o.ClientOrderID, o.Side, o.OrderType, o.State)
		fmt.Printf("       price=%s size=%s filled=%s\n",
			o.Price.String(), o.Size.String(), o.FilledSize.String())
	}
	if len(orders) > 0 && orders[0].RateLimits != nil {
		fmt.Printf("  rate-limit headers: %v\n", orders[0].RateLimits)
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
