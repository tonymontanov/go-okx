/*
FILE: swap/types/order-info.go

DESCRIPTION:
SWAP order information struct. Used as:
  - return value for CreateOrder/ModifyOrder/CancelOrder/Batch*;
  - element in the GetOpenOrders list;
  - payload for WatchOpenOrders events.

FIELDS:
  - OrderID       — exchange order id (ordId).
  - ClientOrderID — client order id (clOrdId).
  - InstID        — instrument.
  - Side          — direction.
  - Price         — order price.
  - Size          — total size (sz, in contracts).
  - FilledSize    — how much has been filled (accFillSz).
  - State         — status (live/partially_filled/filled/canceled).
  - CreatedAtMs   — creation timestamp (cTime, milliseconds).
  - UpdatedAtMs   — last update timestamp (uTime).
  - RateLimits    — snapshot of rate-limit headers received with the response.
    map[header]value.

All decimal values are stored as decimal.Decimal — losslessly. The core adapter
converts them to float64 (.InexactFloat64()) on its side.
*/

package types

import "github.com/shopspring/decimal"

// OrderInfo — SWAP order information.
type OrderInfo struct {
	OrderID       string
	ClientOrderID string
	InstID        string
	Side          SideType
	OrderType     OrderType
	Price         decimal.Decimal
	Size          decimal.Decimal
	FilledSize    decimal.Decimal
	State         OrderState
	CreatedAtMs   int64
	UpdatedAtMs   int64
	RateLimits    map[string]string
}
