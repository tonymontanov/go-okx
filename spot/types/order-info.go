/*
FILE: spot/types/order-info.go

DESCRIPTION:
SPOT order information. Structurally matches SWAP OrderInfo
(see swaptypes.OrderInfo) — the only difference: on SPOT Size is in BASE
currency, not in contracts. A separate type is defined to ensure the Size
field documentation is accurate.
*/

package types

import "github.com/shopspring/decimal"

// OrderInfo — SPOT order information.
type OrderInfo struct {
	// OrderID — exchange order id (ordId).
	OrderID string
	// ClientOrderID — client order id (clOrdId).
	ClientOrderID string
	// InstID — instrument in OKX SPOT format (e.g. "BTC-USDT").
	InstID string
	// Side — order direction.
	Side SideType
	// OrderType — order type.
	OrderType OrderType
	// Price — order price.
	Price decimal.Decimal
	// Size — size in BASE currency (e.g. 0.1 BTC).
	Size decimal.Decimal
	// FilledSize — filled quantity (accFillSz), in BASE currency.
	FilledSize decimal.Decimal
	// State — order status.
	State OrderState
	// CreatedAtMs — creation timestamp (milliseconds).
	CreatedAtMs int64
	// UpdatedAtMs — last update timestamp.
	UpdatedAtMs int64
	// RateLimits — snapshot of rate-limit headers received with the response.
	RateLimits map[string]string
}
