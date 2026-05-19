/*
FILE: types/open-interest.go

DESCRIPTION:
OpenInterest — total open position volume for an instrument in three
equivalent denominations (contracts, base currency, USD).

Mapped from:
  - GET /api/v5/public/open-interest?instType=...|instId=...
  - WS public channel "open-interest"

HFT usage:
  - OI change combined with price movement — indicator of real positional
    flow (vs noise);
  - a sharp OI drop during a price move typically signals liquidations;
    rising OI signals fresh positions being opened.
*/

package types

import "github.com/shopspring/decimal"

// OpenInterest — open interest snapshot.
type OpenInterest struct {
	// InstType — instrument type (SWAP/FUTURES/OPTION).
	InstType InstType
	// InstID — instrument identifier.
	InstID string
	// OI — open interest in contracts (oi).
	OI decimal.Decimal
	// OICcy — open interest in base currency (oiCcy).
	OICcy decimal.Decimal
	// OIUsd — open interest in USD equivalent (oiUsd).
	OIUsd decimal.Decimal
	// Ts — snapshot timestamp in ms (ts).
	Ts int64
}

// OpenInterests — slice.
type OpenInterests []OpenInterest
