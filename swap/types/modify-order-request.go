/*
FILE: swap/types/modify-order-request.go

DESCRIPTION:
SWAP order modification (amend) request struct. OKX amend-order only supports
changing newSz/newPx; side/type/tif cannot be modified — if those need to
change, the order must be cancelled and recreated.

FIELDS:
  - InstID        — instrument.
  - OrderID       — exchange order id (ordId). Either this OR ClientOrderID must be set.
  - ClientOrderID — client order id (clOrdId).
  - NewSize       — new size (optional).
  - NewPrice      — new price (optional).
  - RequestID     — req-id for OKX-side idempotency (optional).

INVARIANTS:
  - Exactly one identifier must be set: OrderID or ClientOrderID.
  - At least one of NewSize/NewPrice must be set; otherwise OKX returns an error.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — SWAP order amend request.
type ModifyOrderRequest struct {
	InstID        string
	OrderID       string
	ClientOrderID string
	NewSize       decimal.Decimal
	NewPrice      decimal.Decimal
	RequestID     string
}
