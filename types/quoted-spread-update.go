/*
FILE: types/quoted-spread-update.go

DESCRIPTION:
Best bid/ask update (spread). Delivered to the WatchSpread subscription callback.
Semantically equivalent to core/types.QuotedSpreadUpdate.

The bbo-tbt channel format is identical for spot and swap in OKX v5.

FIELDS:
  - InstID    — instrument.
  - BestBid   — best buy price.
  - BestBidSz — volume at the best bid.
  - BestAsk   — best sell price.
  - BestAskSz — volume at the best ask.
  - Ts        — OKX timestamp (ms).
*/

package types

import "github.com/shopspring/decimal"

// QuotedSpreadUpdate — best bid/ask update.
type QuotedSpreadUpdate struct {
	InstID    string
	BestBid   decimal.Decimal
	BestBidSz decimal.Decimal
	BestAsk   decimal.Decimal
	BestAskSz decimal.Decimal
	Ts        int64
}
