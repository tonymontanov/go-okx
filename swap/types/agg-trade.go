/*
ФАЙЛ: swap/types/agg-trade.go

ОПИСАНИЕ:
AggTrade для SWAP-профиля. С момента выделения общего слоя — type-alias на
commontypes.AggTrade. Документация — в types/agg-trade.go.

ЕДИНИЦА Size для SWAP — контракты (умножать на ctVal для приведения в base).
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// AggTrade — одна сделка из потока trades. См. commontypes.AggTrade.
type AggTrade = commontypes.AggTrade
