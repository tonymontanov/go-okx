/*
FILE: spot/types/modify-order-request.go

DESCRIPTION:
SPOT order amend (modify) request. Semantically identical to the SWAP variant:
OKX amend changes only newSz/newPx; side/type cannot be changed.

INVARIANTS:
  - Exactly one identifier must be set: OrderID or ClientOrderID.
  - At least one of NewSize/NewPrice must be set.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — SPOT order amend request.
type ModifyOrderRequest struct {
	InstID        string
	OrderID       string
	ClientOrderID string
	NewSize       decimal.Decimal
	NewPrice      decimal.Decimal
	RequestID     string
}
