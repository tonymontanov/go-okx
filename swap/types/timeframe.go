/*
ФАЙЛ: swap/types/timeframe.go

ОПИСАНИЕ:
Таймфрейм исторических свечей для SWAP-профиля. С момента выделения общего
слоя — type-alias на commontypes.Timeframe + реэкспорт констант.
Документация — в types/timeframe.go.
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// Timeframe — таймфрейм свечи. См. commontypes.Timeframe.
type Timeframe = commontypes.Timeframe

const (
	// Sub-minute — поддерживается только агрегацией наверху, REST вернёт ошибку.
	Timeframe1s  = commontypes.Timeframe1s
	Timeframe15s = commontypes.Timeframe15s
	Timeframe30s = commontypes.Timeframe30s

	Timeframe1m  = commontypes.Timeframe1m
	Timeframe3m  = commontypes.Timeframe3m
	Timeframe5m  = commontypes.Timeframe5m
	Timeframe15m = commontypes.Timeframe15m
	Timeframe30m = commontypes.Timeframe30m

	Timeframe1h  = commontypes.Timeframe1h
	Timeframe2h  = commontypes.Timeframe2h
	Timeframe4h  = commontypes.Timeframe4h
	Timeframe6h  = commontypes.Timeframe6h
	Timeframe8h  = commontypes.Timeframe8h
	Timeframe12h = commontypes.Timeframe12h

	Timeframe1D  = commontypes.Timeframe1D
	Timeframe1W  = commontypes.Timeframe1W
	Timeframe1Mo = commontypes.Timeframe1Mo
)
