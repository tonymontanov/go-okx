/*
FILE: spot/types/cancel-order-request.go

DESCRIPTION:
SPOT order cancellation request. Exactly one identifier must be set.
*/

package types

// CancelOrderRequest — SPOT order cancellation request.
type CancelOrderRequest struct {
	InstID        string
	OrderID       string
	ClientOrderID string
}
