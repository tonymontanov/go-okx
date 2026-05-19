/*
FILE: swap/types/position-info.go

DESCRIPTION:
SWAP position information struct.

FIELDS:
  - InstID         — instrument.
  - PosSide        — long/short/net (for hedge-mode scenarios). In net-mode (default)
                     always PosSideNet.
  - Position       — position size in contracts. Sign (+/-) indicates direction in
                     net-mode (+long, -short).
  - AvgEntryPrice  — average entry price.
  - UnrealizedPnL  — unrealized PnL in the margin currency.
  - LiqPrice       — liquidation price (0 if not applicable).
  - UpdatedAtMs    — last update timestamp (uTime).
*/

package types

import "github.com/shopspring/decimal"

// PositionInfo — SWAP position information.
type PositionInfo struct {
	InstID        string
	PosSide       PosSide
	Position      decimal.Decimal
	AvgEntryPrice decimal.Decimal
	UnrealizedPnL decimal.Decimal
	LiqPrice      decimal.Decimal
	UpdatedAtMs   int64
}
