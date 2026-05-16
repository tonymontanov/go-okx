/*
ФАЙЛ: swap/types/candle.go

ОПИСАНИЕ:
Структуры исторической свечи SWAP-профиля. С момента выделения общего слоя
— type-alias на commontypes.Candle/Candles. Документация — в types/candle.go.

ЕДИНИЦА Volume для SWAP — контракты (умножать на ctVal для приведения в base).
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// Candle — одна свеча. См. commontypes.Candle.
type Candle = commontypes.Candle

// Candles — слайс свечей. См. commontypes.Candles.
type Candles = commontypes.Candles
