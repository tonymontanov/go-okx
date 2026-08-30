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
  - RPITakerAccess — rpiTakerAccess flag. OKX does NOT inherit the flag from the
                    original order on amend: an amend request without it resets
                    the order to non-RPI matching, so callers must re-specify it
                    on every amend. The key is emitted only when true.

INVARIANTS:
  - Exactly one identifier must be set: OrderID or ClientOrderID.
  - At least one of NewSize/NewPrice must be set; otherwise OKX returns an error.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — SWAP order amend request.
type ModifyOrderRequest struct {
	InstID         string
	OrderID        string
	ClientOrderID  string
	NewSize        decimal.Decimal
	NewPrice       decimal.Decimal
	RequestID      string
	RPITakerAccess bool
}
