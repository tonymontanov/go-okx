/*
FILE: swap/types/cancel-order-request.go

DESCRIPTION:
SWAP order cancellation request struct. Exactly one identifier must be set:
OrderID or ClientOrderID.
*/

package types

// CancelOrderRequest — SWAP order cancellation request.
type CancelOrderRequest struct {
	InstID        string
	OrderID       string
	ClientOrderID string
}
