/*
FILE: types/candle.go

DESCRIPTION:
Historical candle structs. Mapped from the array of numbers/strings in the
`GET /api/v5/market/candles` and `history-candles` responses.

OKX response format (positional string array):

	[ ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm ]

Where confirm is the candle close flag ("0" — current, "1" — closed).

DIFFERENCES IN Volume SEMANTICS BETWEEN PROFILES — the adapter's responsibility,
not the struct's:
  - spot:  Volume in base currency directly;
  - swap:  Volume in contracts (multiply by ctVal to convert to base).

The struct stores both figures (Volume and VolumeQuote) and leaves the unit
interpretation to the calling layer.
*/

package types

import "github.com/shopspring/decimal"

// Candle — one candle.
type Candle struct {
	OpenTimeMs  int64
	Open        decimal.Decimal
	High        decimal.Decimal
	Low         decimal.Decimal
	Close       decimal.Decimal
	Volume      decimal.Decimal
	VolumeQuote decimal.Decimal
	Closed      bool
}

// Candles — slice of candles.
type Candles []Candle
