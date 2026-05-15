/*
ФАЙЛ: swap/trading_validation_test.go

ОПИСАНИЕ:
Юнит-тесты на клиентскую валидацию запросов в TradingClient. Не делают сетевых
вызовов — проверяют, что SDK отлавливает невалидные входные данные ДО отправки
на биржу и возвращает ErrorKindInvalidRequest.

Главное покрытие — clOrdId. OKX требует случайный case-sensitive alphanumeric
1..32 символа: подчёркивания, дефисы, точки и иные символы отклоняются биржей
кодом 51000 ("Parameter clOrdId error"). Эта проверка должна происходить в
SDK, а не на бирже.
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
				// Validation должна пройти. Запрос всё равно упадёт (mockOKX вернёт
				// 404 на /api/v5/trade/order), но это уже не InvalidRequest от SDK.
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
