/*
FILE: swap/types/order-book-level.go

DESCRIPTION:
Order book level for the SWAP profile. Since the common layer was extracted —
a type alias for commontypes.OrderBookLevel (the format is identical for spot
and swap).

Full documentation is in types/order-book-level.go.

Size unit: for SWAP — OKX contracts (multiply by ctVal to convert to base).
This semantic belongs to the adapter/connector layer, not the struct.
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// OrderBookLevel — one order book level. See commontypes.OrderBookLevel.
type OrderBookLevel = commontypes.OrderBookLevel
