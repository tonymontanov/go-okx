/*
ФАЙЛ: spot/types/modify-order-request.go

ОПИСАНИЕ:
Запрос на amend (modify) ордера SPOT. Идентичен по семантике SWAP-варианту:
OKX amend меняет только newSz/newPx; side/type — нет.

ИНВАРИАНТЫ:
  - Должен быть задан ровно один идентификатор: OrderID или ClientOrderID.
  - Должно быть задано хотя бы одно из NewSize/NewPrice.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — запрос на amend ордера SPOT.
type ModifyOrderRequest struct {
	InstID        string
	OrderID       string
	ClientOrderID string
	NewSize       decimal.Decimal
	NewPrice      decimal.Decimal
	RequestID     string
}
