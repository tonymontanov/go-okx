/*
ФАЙЛ: spot/types/aliases.go

ОПИСАНИЕ:
Type-алиасы протокольно-общих типов OKX v5 для SPOT-профиля. Все эти типы
лежат в нейтральном пакете github.com/tonymontanov/go-okx/v2/types и
переиспользуются обоими профилями (spot и swap) без перекрёстных зависимостей.

ПОЧЕМУ ЗАВИСИМОСТЬ НА github.com/tonymontanov/go-okx/v2/types, А НЕ НА swap/types:
Раньше spot/types был алиасом на swap/types — это создавало логически
некорректную зависимость spot → swap (профили равноправны и не должны знать
друг о друге). Общий слой устраняет проблему: и spot, и swap зависят только
от types, и оба могут существовать независимо.

ПОЧЕМУ АЛИАСЫ, А НЕ ОТДЕЛЬНЫЕ ТИПЫ:
  - `type X = commontypes.X` — это полностью идентичный тип на уровне Go
    (не «новый named type»). spot.CreateOrderRequest{Side: types.SideTypeBuy}
    компилируется без приведений, даже если internal где-то ожидает
    common-версии того же типа.
  - Один источник истины: при изменении/добавлении значений (новый OrderType
    у OKX) правится в одном месте — types/enums.go.
  - Нулевой runtime-cost: алиасы не создают новых typed-значений в бинаре.

SPOT-СПЕЦИФИЧНЫЕ ТИПЫ — в отдельных файлах этого пакета (create-order-request.go,
order-info.go, symbol-info.go и т. д.).
*/

package types

import (
	commontypes "github.com/tonymontanov/go-okx/v2/types"
)

// SideType — направление ордера (buy/sell). См. commontypes.SideType.
type SideType = commontypes.SideType

const (
	SideTypeBuy  = commontypes.SideTypeBuy
	SideTypeSell = commontypes.SideTypeSell
)

// OrderType — тип ордера в нотации OKX. См. commontypes.OrderType.
// Для SPOT применимы: market, limit, post_only, fok, ioc.
// OrderTypeOptimalLimitIOC — SWAP-only константа, в spot/types её нет.
type OrderType = commontypes.OrderType

const (
	OrderTypeMarket   = commontypes.OrderTypeMarket
	OrderTypeLimit    = commontypes.OrderTypeLimit
	OrderTypePostOnly = commontypes.OrderTypePostOnly
	OrderTypeFOK      = commontypes.OrderTypeFOK
	OrderTypeIOC      = commontypes.OrderTypeIOC
)

// TimeInForceType — TIF (Binance-style). См. commontypes.TimeInForceType.
type TimeInForceType = commontypes.TimeInForceType

const (
	TimeInForceTypeGTC = commontypes.TimeInForceTypeGTC
	TimeInForceTypeIOC = commontypes.TimeInForceTypeIOC
	TimeInForceTypeFOK = commontypes.TimeInForceTypeFOK
	TimeInForceTypeGTX = commontypes.TimeInForceTypeGTX
)

// TdMode — margin-mode ордера. См. commontypes.TdMode.
// Для SPOT обычно используется TdModeCash (без плеча). TdModeCross/Isolated
// доступны для spot margin trading, но в текущей версии SDK не используются
// (см. doc.go).
type TdMode = commontypes.TdMode

const (
	TdModeCross    = commontypes.TdModeCross
	TdModeIsolated = commontypes.TdModeIsolated
	TdModeCash     = commontypes.TdModeCash
)

// InstType — тип инструмента OKX. SPOT-профиль всегда отдаёт InstTypeSpot.
type InstType = commontypes.InstType

const (
	InstTypeSpot    = commontypes.InstTypeSpot
	InstTypeSWAP    = commontypes.InstTypeSWAP
	InstTypeFutures = commontypes.InstTypeFutures
	InstTypeOption  = commontypes.InstTypeOption
)

// OrderState — статус ордера. См. commontypes.OrderState.
type OrderState = commontypes.OrderState

const (
	OrderStateLive            = commontypes.OrderStateLive
	OrderStatePartiallyFilled = commontypes.OrderStatePartiallyFilled
	OrderStateFilled          = commontypes.OrderStateFilled
	OrderStateCanceled        = commontypes.OrderStateCanceled
	OrderStateUnknown         = commontypes.OrderStateUnknown
)

// ParseOrderState — реэкспорт.
var ParseOrderState = commontypes.ParseOrderState

// Общие модели данных — orderbook level/snapshot, candles, agg trades.
// Формат идентичен для spot и swap (REST/WS endpoints общие), поэтому
// переиспользуем через алиасы на общий пакет.

// OrderBookLevel — уровень стакана.
type OrderBookLevel = commontypes.OrderBookLevel

// OrderBookSnapshot — снимок стакана.
type OrderBookSnapshot = commontypes.OrderBookSnapshot

// Candle — одна свеча.
type Candle = commontypes.Candle

// Candles — слайс свечей.
type Candles = commontypes.Candles

// Timeframe — таймфрейм запроса свечей.
type Timeframe = commontypes.Timeframe

// Реэкспорт констант Timeframe — чтобы пользователь spot мог писать
// types.Timeframe1m без отдельного импорта общего пакета.
const (
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

// AggTrade — агрегированная сделка из WS канала trades.
type AggTrade = commontypes.AggTrade

// QuotedSpreadUpdate — BBO из канала bbo-tbt.
type QuotedSpreadUpdate = commontypes.QuotedSpreadUpdate

// Balance — состояние unified-account (общее для spot и swap; OKX отдаёт
// один баланс на пользователя).
type Balance = commontypes.Balance

// BalanceDetail — баланс одной валюты внутри unified-account.
type BalanceDetail = commontypes.BalanceDetail
