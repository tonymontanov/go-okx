/*
FILE: types/index-ticker.go

DESCRIPTION:
IndexTicker — index price and 24h metrics for an OKX index (e.g.
BTC-USDT-INDEX, ETH-USDT-INDEX). Used as a reference for perp, delivery,
and options products.

Mapped from:
  - GET /api/v5/market/index-tickers?quoteCcy=...|instId=...
  - WS public channel "index-tickers"

Unlike a regular ticker there is no ask/bid/last here — an index is a
calculated value, not a tradable instrument.
*/

package types

import "github.com/shopspring/decimal"

// IndexTicker — index price snapshot.
type IndexTicker struct {
	// InstID — index identifier (e.g. "BTC-USDT-INDEX").
	InstID string
	// IdxPx — current index price (idxPx).
	IdxPx decimal.Decimal
	// Open24h — index price 24h ago (open24h).
	Open24h decimal.Decimal
	// High24h — highest index price over 24h (high24h).
	High24h decimal.Decimal
	// Low24h — lowest index price over 24h (low24h).
	Low24h decimal.Decimal
	// SodUtc0 — index at 00:00 UTC (sodUtc0).
	SodUtc0 decimal.Decimal
	// SodUtc8 — index at 08:00 UTC (sodUtc8).
	SodUtc8 decimal.Decimal
	// Ts — timestamp in ms (ts).
	Ts int64
}

// IndexTickers — slice of index tickers.
type IndexTickers []IndexTicker
