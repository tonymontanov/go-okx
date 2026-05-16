/*
ФАЙЛ: spot/types/cancel-order-request.go

ОПИСАНИЕ:
Запрос на отмену ордера SPOT. Должен быть задан ровно один из идентификаторов.
*/

package types

// CancelOrderRequest — запрос на отмену ордера SPOT.
type CancelOrderRequest struct {
	InstID        string
	OrderID       string
	ClientOrderID string
}
