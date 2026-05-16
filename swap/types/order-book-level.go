/*
ФАЙЛ: swap/types/order-book-level.go

ОПИСАНИЕ:
Уровень стакана для SWAP-профиля. С момента выделения общего слоя
github.com/tonymontanov/go-okx/v2/types — это type-alias на
commontypes.OrderBookLevel (формат идентичен для spot и swap).

Полная документация — в types/order-book-level.go.

ЕДИНИЦА Size: для SWAP — контракты OKX (умножать на ctVal для приведения
в base). Эта семантика на уровне адаптера/коннектора, не структуры.
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// OrderBookLevel — один уровень стакана. См. commontypes.OrderBookLevel.
type OrderBookLevel = commontypes.OrderBookLevel
