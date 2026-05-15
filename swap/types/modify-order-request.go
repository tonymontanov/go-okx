/*
ФАЙЛ: swap/types/modify-order-request.go

ОПИСАНИЕ:
Структура запроса на модификацию (amend) ордера SWAP. OKX amend-order
поддерживает изменение только newSz/newPx; side/type/tif не модифицируются —
если нужно изменить их, ордер придётся отменить и создать заново.

ПОЛЯ:
  - InstID        — инструмент.
  - OrderID       — биржевой id ордера (ordId). Заполняется ЛИБО он, ЛИБО ClientOrderID.
  - ClientOrderID — клиентский id (clOrdId).
  - NewSize       — новый размер (опционально).
  - NewPrice      — новая цена (опционально).
  - RequestID     — req-id для идемпотентности на стороне OKX (опционально).

ИНВАРИАНТЫ:
  - Должен быть задан ровно один идентификатор: OrderID или ClientOrderID.
  - Должно быть задано хотя бы одно из NewSize/NewPrice; иначе OKX вернёт ошибку.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — запрос на amend ордера SWAP.
type ModifyOrderRequest struct {
	InstID        string
	OrderID       string
	ClientOrderID string
	NewSize       decimal.Decimal
	NewPrice      decimal.Decimal
	RequestID     string
}
