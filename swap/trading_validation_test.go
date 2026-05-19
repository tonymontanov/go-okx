/*
FILE: swap/trading_validation_test.go

DESCRIPTION:
Unit tests for client-side request validation in TradingClient. No network calls
are made — they verify that the SDK catches invalid input BEFORE sending to the
exchange and returns ErrorKindInvalidRequest.

Main coverage — clOrdId. OKX requires a case-sensitive alphanumeric string of
1..32 characters: underscores, hyphens, dots, and other characters are rejected
by the exchange with code 51000 ("Parameter clOrdId error"). This check must
happen in the SDK, not at the exchange.
*/

package swap

import (
	"context"
	"testing"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

func TestCreateOrder_RejectsInvalidClientOrderID(t *testing.T) {
	var cases = []struct {
		name    string
		clOrdID string
		ok      bool
	}{
		{name: "alpha", clOrdID: "abc", ok: true},
		{name: "alphaNumeric", clOrdID: "ex1747333333333", ok: true},
		{name: "mixedCase", clOrdID: "myOrderXYZ123", ok: true},
		{name: "max32", clOrdID: "abcdefghijklmnopqrstuvwxyz123456", ok: true},
		{name: "underscore", clOrdID: "ex_1234", ok: false},
		{name: "dash", clOrdID: "ex-1234", ok: false},
		{name: "dot", clOrdID: "ex.1234", ok: false},
		{name: "space", clOrdID: "ex 1234", ok: false},
		{name: "tooLong33", clOrdID: "abcdefghijklmnopqrstuvwxyz1234567", ok: false},
		{name: "empty allowed", clOrdID: "", ok: true},
	}

	var _, client = mockOKX(t, map[string]string{})

	var i int
	for i = 0; i < len(cases); i++ {
		var c = cases[i]
		t.Run(c.name, func(t *testing.T) {
			var _, err = swapOf(client).Trading().CreateOrder(context.Background(), types.CreateOrderRequest{
				InstID:        "BTC-USDT-SWAP",
				Side:          types.SideTypeBuy,
				OrderType:     types.OrderTypeLimit,
				Price:         mustDec("10000"),
				Size:          mustDec("1"),
				ClientOrderID: c.clOrdID,
			})
			if c.ok {
				// Validation must pass. The request will still fail (mockOKX returns
				// 404 for /api/v5/trade/order), but that is not an InvalidRequest from the SDK.
				if okx.IsInvalidRequest(err) {
					t.Fatalf("expected validation to pass, got InvalidRequest: %v", err)
				}
			} else {
				if !okx.IsInvalidRequest(err) {
					t.Fatalf("expected ErrorKindInvalidRequest, got %v", err)
				}
			}
		})
	}
}
