/*
FILE: examples/simple-trade/main.go

DESCRIPTION:
Minimal REST trading scenario:
  1. read keys from ENV (OKX_API_KEY / OKX_SECRET_KEY / OKX_PASSPHRASE);
  2. place a limit order far from the market (so it does not fill);
  3. modify the size;
  4. cancel;
  5. on exit print the result and rate-limit headers.

WARNING:
    The example uses the production endpoint. Without demo mode (out of v1 scope)
    it will actually send orders. To guard against accidental runs, set
    OKX_ALLOW_LIVE=1 explicitly.

RUN:
    export OKX_API_KEY=...
    export OKX_SECRET_KEY=...
    export OKX_PASSPHRASE=...
    export OKX_ALLOW_LIVE=1
    go run ./examples/simple-trade
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shopspring/decimal"
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
	if os.Getenv("OKX_ALLOW_LIVE") != "1" {
		log.Fatal("refusing to trade against production: set OKX_ALLOW_LIVE=1 explicitly")
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

	placeOrder(ctx, swap)
}

// placeOrder demonstrates the full happy-path: place → modify → cancel.
// All decimal values are created explicitly — no float literals.
func placeOrder(ctx context.Context, swap *swappkg.Client) {
	var price decimal.Decimal = decimal.RequireFromString("10000")
	var size decimal.Decimal = decimal.RequireFromString("1")

	var info types.OrderInfo
	var err error
	info, err = swap.Trading().CreateOrder(ctx, types.CreateOrderRequest{
		InstID:        "BTC-USDT-SWAP",
		Side:          types.SideTypeBuy,
		OrderType:     types.OrderTypeLimit,
		Price:         price,
		Size:          size,
		ClientOrderID: "ex" + fmt.Sprint(time.Now().UnixMilli()),
	})
	if err != nil {
		log.Fatalf("CreateOrder: %v", classify(err))
	}
	fmt.Printf("placed: ordId=%s clOrdId=%s rate-limit=%v\n", info.OrderID, info.ClientOrderID, info.RateLimits)

	// Modify size.
	var newSize decimal.Decimal = decimal.RequireFromString("2")
	_, err = swap.Trading().ModifyOrder(ctx, types.ModifyOrderRequest{
		InstID:  info.InstID,
		OrderID: info.OrderID,
		NewSize: newSize,
	})
	if err != nil {
		log.Printf("ModifyOrder: %v", classify(err))
	} else {
		fmt.Println("modified ok")
	}

	// Cancel.
	err = swap.Trading().CancelOrder(ctx, types.CancelOrderRequest{
		InstID:  info.InstID,
		OrderID: info.OrderID,
	})
	if err != nil {
		log.Printf("CancelOrder: %v", classify(err))
	} else {
		fmt.Println("cancelled ok")
	}
}

// classify converts an SDK error to a human-readable form with category.
// Includes Cause — without it network problems look like "rest: transport error"
// with no details.
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
