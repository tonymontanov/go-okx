/*
ФАЙЛ: spot/types/aliases.go

ОПИСАНИЕ:
Type-алиасы для enum'ов, общих между SPOT и SWAP профилями OKX
(SideType, OrderType, TimeInForceType, TdMode, InstType, OrderState, ...).
Эти enum'ы строго совпадают с протоколом OKX v5 и не зависят от профиля —
поэтому переопределять их в spot/types бессмысленно.

ПОЧЕМУ АЛИАСЫ, А НЕ ОТДЕЛЬНЫЕ ТИПЫ:
  - `type X = swaptypes.X` — это полностью идентичный тип на уровне Go
    (не "новый named type"). spot.CreateOrderRequest{Side: types.SideTypeBuy}
    компилируется без приведений, даже если internal в SDK где-то ожидает
    swap-version'ию того же типа.
  - Один источник истины: при изменении/добавлении значений (новый OrderType
    у OKX) правится в одном месте — swap/types/enums.go.
  - Нулевой runtime-cost: алиасы не создают новых typed-значений в бинаре.

ПОЧЕМУ ЗАВИСИМОСТЬ spot → swap, А НЕ ОБЩИЙ ПАКЕТ:
  - Сейчас профилей два, общий пакет (например `okx/types/common/`) — лишняя
    абстракция. При появлении третьего (margin, options) — выделим, без
    breaking change для пользователей spot/swap (алиасы продолжат работать).
  - swap/types/ — стабильный публичный пакет уже с v2.0.0, переезд enum'ов
    в новый пакет был бы breaking change для существующих пользователей.

SPOT-СПЕЦИФИЧНЫЕ ТИПЫ — в отдельных файлах этого пакета (create-order-request.go,
order-info.go, symbol-info.go и т.д.).
*/

package types

import (
	swaptypes "github.com/tonymontanov/go-okx/v2/swap/types"
)

// SideType — направление ордера (buy/sell). См. swaptypes.SideType.
type SideType = swaptypes.SideType

// Side constants — реэкспорт констант для удобства (var, чтобы их можно было
// использовать без префикса пакета swaptypes.).
const (
	SideTypeBuy  = swaptypes.SideTypeBuy
	SideTypeSell = swaptypes.SideTypeSell
)

// OrderType — тип ордера в нотации OKX. См. swaptypes.OrderType.
// Для SPOT применимы: market, limit, post_only, fok, ioc.
// OrderTypeOptimalLimitIOC — только для SWAP, на SPOT OKX вернёт ошибку.
type OrderType = swaptypes.OrderType

const (
	OrderTypeMarket   = swaptypes.OrderTypeMarket
	OrderTypeLimit    = swaptypes.OrderTypeLimit
	OrderTypePostOnly = swaptypes.OrderTypePostOnly
	OrderTypeFOK      = swaptypes.OrderTypeFOK
	OrderTypeIOC      = swaptypes.OrderTypeIOC
)

// TimeInForceType — TIF (Binance-style). См. swaptypes.TimeInForceType.
type TimeInForceType = swaptypes.TimeInForceType

const (
	TimeInForceTypeGTC = swaptypes.TimeInForceTypeGTC
	TimeInForceTypeIOC = swaptypes.TimeInForceTypeIOC
	TimeInForceTypeFOK = swaptypes.TimeInForceTypeFOK
	TimeInForceTypeGTX = swaptypes.TimeInForceTypeGTX
)

// TdMode — margin-mode ордера. См. swaptypes.TdMode.
// Для SPOT обычно используется TdModeCash (без плеча). TdModeCross/Isolated
// доступны для spot margin trading, но в текущей версии SDK не используются
// (см. doc.go).
type TdMode = swaptypes.TdMode

const (
	TdModeCross    = swaptypes.TdModeCross
	TdModeIsolated = swaptypes.TdModeIsolated
	TdModeCash     = swaptypes.TdModeCash
)

// InstType — тип инструмента OKX. SPOT-профиль всегда отдаёт InstTypeSpot.
type InstType = swaptypes.InstType

const (
	InstTypeSpot    = swaptypes.InstTypeSpot
	InstTypeSWAP    = swaptypes.InstTypeSWAP
	InstTypeFutures = swaptypes.InstTypeFutures
	InstTypeOption  = swaptypes.InstTypeOption
)

// OrderState — статус ордера. См. swaptypes.OrderState.
type OrderState = swaptypes.OrderState

const (
	OrderStateLive            = swaptypes.OrderStateLive
	OrderStatePartiallyFilled = swaptypes.OrderStatePartiallyFilled
	OrderStateFilled          = swaptypes.OrderStateFilled
	OrderStateCanceled        = swaptypes.OrderStateCanceled
	OrderStateUnknown         = swaptypes.OrderStateUnknown
)

// ParseOrderState — реэкспорт.
var ParseOrderState = swaptypes.ParseOrderState

// Общие модели данных — orderbook level и snapshot, candles, agg trades.
// Они одного формата у SPOT и SWAP в OKX v5 (channels/REST совместимы),
// поэтому переиспользуем напрямую через алиасы.

// OrderBookLevel — уровень стакана.
type OrderBookLevel = swaptypes.OrderBookLevel

// OrderBookSnapshot — снимок стакана.
type OrderBookSnapshot = swaptypes.OrderBookSnapshot

// Candle — одна свеча.
type Candle = swaptypes.Candle

// Candles — слайс свечей.
type Candles = swaptypes.Candles

// Timeframe — таймфрейм запроса свечей.
type Timeframe = swaptypes.Timeframe

// Реэкспорт констант Timeframe чтобы пользователь spot мог делать
// types.Timeframe1m без импорта swap/types.
const (
	Timeframe1s  = swaptypes.Timeframe1s
	Timeframe15s = swaptypes.Timeframe15s
	Timeframe30s = swaptypes.Timeframe30s
	Timeframe1m  = swaptypes.Timeframe1m
	Timeframe3m  = swaptypes.Timeframe3m
	Timeframe5m  = swaptypes.Timeframe5m
	Timeframe15m = swaptypes.Timeframe15m
	Timeframe30m = swaptypes.Timeframe30m
	Timeframe1h  = swaptypes.Timeframe1h
	Timeframe2h  = swaptypes.Timeframe2h
	Timeframe4h  = swaptypes.Timeframe4h
	Timeframe6h  = swaptypes.Timeframe6h
	Timeframe8h  = swaptypes.Timeframe8h
	Timeframe12h = swaptypes.Timeframe12h
	Timeframe1D  = swaptypes.Timeframe1D
	Timeframe1W  = swaptypes.Timeframe1W
	Timeframe1Mo = swaptypes.Timeframe1Mo
)

// AggTrade — агрегированная сделка из WS канала trades.
type AggTrade = swaptypes.AggTrade

// QuotedSpreadUpdate — BBO из канала bbo-tbt. На спотовых инструментах OKX
// канал доступен с той же семантикой.
type QuotedSpreadUpdate = swaptypes.QuotedSpreadUpdate

// Balance — состояние unified-account целиком (топ-уровень + per-currency).
//
// OKX использует ОДИН unified-account на пользователя: spot и swap балансы
// возвращаются одним и тем же endpoint'ом /api/v5/account/balance. Поэтому
// тип Balance общий — реэкспорт через алиас.
type Balance = swaptypes.Balance

// BalanceDetail — баланс одной валюты внутри unified-account.
type BalanceDetail = swaptypes.BalanceDetail
