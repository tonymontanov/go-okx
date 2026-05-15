/*
ФАЙЛ: swap/types/cancel-order-request.go

ОПИСАНИЕ:
Структура запроса на отмену ордера SWAP. Должен быть задан ровно один из
идентификаторов: OrderID или ClientOrderID.
*/

package types

// CancelOrderRequest — запрос на отмену ордера SWAP.
type CancelOrderRequest struct {
	InstID        string
	OrderID       string
	ClientOrderID string
}
