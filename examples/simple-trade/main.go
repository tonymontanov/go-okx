/*
ФАЙЛ: examples/simple-trade/main.go

ОПИСАНИЕ:
Минимальный сценарий торговли через REST:
  1. читаем ключи из ENV (OKX_API_KEY / OKX_SECRET_KEY / OKX_PASSPHRASE);
  2. ставим лимитный ордер сильно вдалеке от рынка (чтобы не исполнился);
  3. модифицируем размер;
  4. отменяем;
  5. на выходе печатаем итог и rate-limit заголовки.

ВНИМАНИЕ:
    Пример использует production endpoint. Без demo-режима (он вне Scope v1)
    он реально отправит ордера. Перед запуском уберите комментарии «//» в
    блоке с placeOrder, если действительно готовы.

ЗАПУСК:
    OKX_API_KEY=... OKX_SECRET_KEY=... OKX_PASSPHRASE=... \
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

	// Раскомментируйте перед реальным запуском.
	_ = ctx
	_ = placeOrder
	_ = swap
	// placeOrder(ctx, swap)
}

// placeOrder показывает полный happy-path: place → modify → cancel.
// Все decimal-значения создаются явно — никаких float-литералов.
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
		ClientOrderID: "example_" + fmt.Sprint(time.Now().UnixMilli()),
	})
	if err != nil {
		log.Fatalf("CreateOrder: %v", classify(err))
	}
	fmt.Printf("placed: ordId=%s clOrdId=%s rate-limit=%v\n", info.OrderID, info.ClientOrderID, info.RateLimits)

	// Изменим размер.
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

	// Отменим.
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

// classify приводит ошибку SDK к человеко-читаемой форме с категорией.
func classify(err error) string {
	if err == nil {
		return "<nil>"
	}
	var e *okx.Error
	if errors.As(err, &e) {
		return fmt.Sprintf("[%s code=%s status=%d] %s", e.Kind, e.OKXCode, e.HTTPStatus, e.Message)
	}
	return err.Error()
}
