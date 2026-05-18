/*
ФАЙЛ: types/leverage-info.go

ОПИСАНИЕ:
LeverageInfo — установленное плечо для инструмента в определённом режиме
маржи (cross/isolated) и стороне позиции (long/short/net).

Маппится из:
  - GET /api/v5/account/leverage-info?instId=...&mgnMode=...

Один запрос может вернуть несколько записей, если у инструмента
установлены разные плечи под разные posSide (long_short_mode).
*/

package types

import "github.com/shopspring/decimal"

// LeverageInfo — текущее плечо.
type LeverageInfo struct {
	// InstID — инструмент.
	InstID string
	// MgnMode — режим маржи ("cross" / "isolated") (mgnMode).
	MgnMode string
	// PosSide — сторона позиции (long/short/net) (posSide).
	PosSide string
	// Lever — установленное плечо (lever).
	Lever decimal.Decimal
}

// LeverageInfos — слайс.
type LeverageInfos []LeverageInfo
