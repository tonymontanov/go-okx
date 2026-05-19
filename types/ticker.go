/*
FILE: types/ticker.go

DESCRIPTION:
Domain model of a ticker: 24h snapshot for a single instrument, with last-trade,
top-of-book, and aggregated OHLCV metrics for the day.

Mapped from:
  - GET /api/v5/market/ticker?instId=...
  - GET /api/v5/market/tickers?instType=...
  - WS public channel "tickers"

FIELDS:
Field semantics follow OKX naming (instId, last, askPx, bidPx, vol24h,
volCcy24h, sodUtc0/8) but in decimal form. Units:
  - Last, AskPx, BidPx, Open24h, High24h, Low24h, SodUtc0, SodUtc8 —
    price in quote currency.
  - LastSz, AskSz, BidSz — size in contracts (SWAP/FUTURES) or base currency
    (SPOT/MARGIN). The adapter decides whether to multiply by ctVal.
  - Vol24h — volume in contracts (SWAP/FUTURES) or base ccy (SPOT).
  - VolCcy24h — volume in quote ccy (SPOT) or base ccy (SWAP/FUTURES).

NOTE:
OKX computes the 24h window as rolling; sodUtc0/sodUtc8 are "open at start
of day" in the given TZ. SodUtc8 (Asia TZ) is useful for arbitrage/premium
calculations; SodUtc0 for reporting.
*/

package types

import "github.com/shopspring/decimal"

// Ticker — 24h ticker snapshot for an instrument.
type Ticker struct {
	// InstType — instrument type (SPOT/SWAP/FUTURES/OPTION/MARGIN).
	InstType InstType
	// InstID — instrument identifier.
	InstID string
	// Last — last trade price (last).
	Last decimal.Decimal
	// LastSz — last trade size (lastSz).
	LastSz decimal.Decimal
	// AskPx — best ask price (askPx).
	AskPx decimal.Decimal
	// AskSz — size at best ask (askSz).
	AskSz decimal.Decimal
	// BidPx — best bid price (bidPx).
	BidPx decimal.Decimal
	// BidSz — size at best bid (bidSz).
	BidSz decimal.Decimal
	// Open24h — open price over the last 24 hours (open24h).
	Open24h decimal.Decimal
	// High24h — highest price over 24 hours (high24h).
	High24h decimal.Decimal
	// Low24h — lowest price over 24 hours (low24h).
	Low24h decimal.Decimal
	// Vol24h — 24-hour volume in base currency/contracts (vol24h).
	Vol24h decimal.Decimal
	// VolCcy24h — 24-hour volume in quote currency (volCcy24h).
	VolCcy24h decimal.Decimal
	// SodUtc0 — open price at 00:00 UTC of the current day (sodUtc0).
	SodUtc0 decimal.Decimal
	// SodUtc8 — open price at 08:00 UTC (00:00 Asia/HK) (sodUtc8).
	SodUtc8 decimal.Decimal
	// Ts — snapshot timestamp in milliseconds (ts).
	Ts int64
}

// Tickers — slice of tickers (for GET /api/v5/market/tickers).
type Tickers []Ticker
