/*
FILE: types/leverage-info.go

DESCRIPTION:
LeverageInfo — configured leverage for an instrument in a given margin mode
(cross/isolated) and position side (long/short/net).

Mapped from:
  - GET /api/v5/account/leverage-info?instId=...&mgnMode=...

A single request may return multiple records if different leverages are
configured for different posSide values (long_short_mode).
*/

package types

import "github.com/shopspring/decimal"

// LeverageInfo — current leverage setting.
type LeverageInfo struct {
	// InstID — instrument.
	InstID string
	// MgnMode — margin mode ("cross" / "isolated") (mgnMode).
	MgnMode string
	// PosSide — position side (long/short/net) (posSide).
	PosSide string
	// Lever — configured leverage (lever).
	Lever decimal.Decimal
}

// LeverageInfos — slice.
type LeverageInfos []LeverageInfo
