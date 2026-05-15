/*
ФАЙЛ: swap/types/position-info.go

ОПИСАНИЕ:
Структура с информацией о позиции SWAP.

ПОЛЯ:
  - InstID         — инструмент.
  - PosSide        — long/short/net (для hedge-mode сценариев). В net-mode (default)
                     всегда PosSideNet.
  - Position       — размер позиции в контрактах. Знак (+/-) соответствует
                     направлению в net-mode (+long, -short).
  - AvgEntryPrice  — средняя цена входа.
  - UnrealizedPnL  — нереализованный PnL в маржинальной валюте.
  - LiqPrice       — ликвидационная цена (0 если не применимо).
  - UpdatedAtMs    — таймштамп последнего обновления (uTime).
*/

package types

import "github.com/shopspring/decimal"

// PositionInfo — информация о позиции SWAP.
type PositionInfo struct {
	InstID        string
	PosSide       PosSide
	Position      decimal.Decimal
	AvgEntryPrice decimal.Decimal
	UnrealizedPnL decimal.Decimal
	LiqPrice      decimal.Decimal
	UpdatedAtMs   int64
}
