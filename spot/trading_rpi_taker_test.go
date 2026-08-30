/*
FILE: spot/trading_rpi_taker_test.go

DESCRIPTION:
Unit tests for the rpiTakerAccess wire key in the SPOT body builders. No
network calls — only the body shape is verified:
  - create: the key is present when RPITakerAccess=true, absent otherwise;
  - amend: OKX does not inherit the flag from the original order, so the
    builder must re-emit it on every amend request.
*/

package spot

import (
	"testing"

	"github.com/tonymontanov/go-okx/v2/spot/types"
)

func TestBuildCreateOrderBody_RPITakerAccess(t *testing.T) {
	var trader *TradingClient = newTradingClient(nil)

	var body, err = trader.buildCreateOrderBody(types.CreateOrderRequest{
		InstID:         "BTC-USDT",
		Side:           types.SideTypeBuy,
		OrderType:      types.OrderTypeIOC,
		Price:          mustDec("10000"),
		Size:           mustDec("1"),
		RPITakerAccess: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body["rpiTakerAccess"]; got != true {
		t.Fatalf("rpiTakerAccess: got %v, want true", got)
	}

	body, err = trader.buildCreateOrderBody(types.CreateOrderRequest{
		InstID:    "BTC-USDT",
		Side:      types.SideTypeBuy,
		OrderType: types.OrderTypeIOC,
		Price:     mustDec("10000"),
		Size:      mustDec("1"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := body["rpiTakerAccess"]; ok {
		t.Fatalf("rpiTakerAccess key must be absent when the flag is false, got %v", body)
	}
}

func TestBuildAmendOrderBody_RPITakerAccess(t *testing.T) {
	var body, err = buildAmendOrderBody(types.ModifyOrderRequest{
		InstID:         "BTC-USDT",
		OrderID:        "123",
		NewPrice:       mustDec("10001"),
		RPITakerAccess: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body["rpiTakerAccess"]; got != true {
		t.Fatalf("rpiTakerAccess: got %v, want true", got)
	}

	body, err = buildAmendOrderBody(types.ModifyOrderRequest{
		InstID:   "BTC-USDT",
		OrderID:  "123",
		NewPrice: mustDec("10001"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := body["rpiTakerAccess"]; ok {
		t.Fatalf("rpiTakerAccess key must be absent when the flag is false, got %v", body)
	}
}
