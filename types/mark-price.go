/*
FILE: types/mark-price.go

DESCRIPTION:
MarkPrice — the fair (mark) price of an instrument, which OKX uses to
calculate PnL for perp/futures/options and to trigger liquidations. Differs
from the last-trade price: smoothed through a combination of index and spread.

Mapped from:
  - GET /api/v5/public/mark-price?instType=...|instId=...
  - WS public channel "mark-price"

HFT usage:
  - mark price is used for unrealized PnL and margin ratio calculations;
  - a sharp divergence (last - mark) signals a tradable dispersion
    (premium/discount to the index).
*/

package types

import "github.com/shopspring/decimal"

// MarkPrice — mark price snapshot.
type MarkPrice struct {
	// InstType — instrument type (SWAP/FUTURES/OPTION).
	InstType InstType
	// InstID — instrument identifier.
	InstID string
	// MarkPx — current mark price (markPx).
	MarkPx decimal.Decimal
	// Ts — snapshot timestamp in ms (ts).
	Ts int64
}

// MarkPrices — slice of mark prices.
type MarkPrices []MarkPrice
