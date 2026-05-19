/*
FILE: types/order-book-level.go

DESCRIPTION:
A single order book level (one depth point: price + size). Used in:
  - REST snapshot (GetOrderBook);
  - orderbook engine (snapshot + delta);
  - WS push (Watch* functions).

The level format is identical for spot and swap in OKX v5 (only the unit
of size differs: spot = base currency, swap = contracts — that is the
adapter's concern, not the struct's).

FIELDS:
  - Price — level price. decimal.Decimal — lossless and comparable without epsilon tricks.
  - Size  — volume at this level (base ccy for spot / contracts for swap).

NOTE:
The `Orders` field (number of orders at the level) that OKX includes in the array
is intentionally OMITTED from the public struct — it is rarely used
in actual trading but doubles the struct size. It will be added in a separate
extended type OrderBookLevelDetailed if needed.
*/

package types

import "github.com/shopspring/decimal"

// OrderBookLevel — one order book level.
type OrderBookLevel struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}
