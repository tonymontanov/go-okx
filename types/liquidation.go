/*
FILE: types/liquidation.go

DESCRIPTION:
Liquidation — a single position liquidation record on the exchange.

Mapped from:
  - GET /api/v5/public/liquidation-orders?instType=...&...
  - WS public channel "liquidation-orders"

NOTE ON AGGREGATION:
In the WS channel OKX typically aggregates liquidations by instrument
(the envelope contains instId followed by a details array). At the SDK level
we flatten to []Liquidation and deliver to the domain layer.

HFT usage:
  - cascading liquidation signal: a series of Liquidation events in a short
    interval → anticipate spike-volatility/reversal;
  - fade the over-leveraged side: track the sell-vs-buy balance of
    liquidations in a window.
*/

package types

import "github.com/shopspring/decimal"

// Liquidation — one liquidation record.
type Liquidation struct {
	// InstType — instrument type (SWAP/FUTURES/MARGIN/OPTION).
	InstType InstType
	// InstID — instrument identifier.
	InstID string
	// Ccy — currency (for MARGIN; empty for others).
	Ccy string
	// Side — taker side of the liquidation (buy = short being liquidated,
	// sell = long being liquidated).
	Side SideType
	// PosSide — position side (long/short/net). In one-way mode will be
	// net; in long-short mode — long or short.
	PosSide string
	// Sz — liquidated position size in contracts (sz).
	Sz decimal.Decimal
	// BankruptcyPx — bankruptcy price (bkPx): the price at which position
	// equity equals zero.
	BankruptcyPx decimal.Decimal
	// BankruptcyLoss — total loss covered by the insurance fund
	// (bkLoss). 0 if the liquidation completed without bankruptcy.
	BankruptcyLoss decimal.Decimal
	// Ts — liquidation timestamp in ms (ts).
	Ts int64
}

// Liquidations — slice of liquidations.
type Liquidations []Liquidation
