/*
FILE: swap/types/order-book-snapshot.go

DESCRIPTION:
Order book snapshot for the SWAP profile. Since the common layer was extracted —
a type alias for commontypes.OrderBookSnapshot. Documentation is in
types/order-book-snapshot.go.
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// OrderBookSnapshot — order book snapshot. See commontypes.OrderBookSnapshot.
type OrderBookSnapshot = commontypes.OrderBookSnapshot
