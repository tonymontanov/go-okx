/*
FILE: types/agg-trade.go

DESCRIPTION:
AggTrade — a single aggregated trade from the WS channel `trades`. Unlike Binance,
OKX does not perform explicit aggregation — every publicly visible trade appears
individually in the `trades` channel. The same name is used for compatibility with
core (which has an AggTrade type), but with decimal fields.

The trades channel format is identical for spot and swap in OKX v5 — only the
semantics of Size differ (base ccy for spot / contracts for swap), which is resolved
by the adapter.

FIELDS:
  - InstID       — instrument.
  - TradeID      — unique trade id.
  - Price        — trade price.
  - Size         — volume (base ccy for spot / contracts for swap).
  - Side         — taker direction (buy/sell).
  - IsBuyerMaker — true if the maker is the buyer (for core compatibility).
  - Ts           — timestamp (ms).
*/

package types

import "github.com/shopspring/decimal"

// AggTrade — one trade from the trades stream.
type AggTrade struct {
	InstID       string
	TradeID      string
	Price        decimal.Decimal
	Size         decimal.Decimal
	Side         SideType
	IsBuyerMaker bool
	Ts           int64
}
