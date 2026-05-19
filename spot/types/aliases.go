/*
FILE: spot/types/aliases.go

DESCRIPTION:
Type aliases for protocol-shared OKX v5 types in the SPOT profile. All these
types reside in the neutral package github.com/tonymontanov/go-okx/v2/types and
are reused by both profiles (spot and swap) without cross-dependencies.

WHY DEPEND ON github.com/tonymontanov/go-okx/v2/types AND NOT swap/types:
Previously spot/types aliased swap/types — this created a logically incorrect
spot → swap dependency (profiles are peers and must not know about each other).
The common layer eliminates the problem: both spot and swap depend only on types
and can exist independently.

WHY ALIASES RATHER THAN SEPARATE TYPES:
  - `type X = commontypes.X` is a fully identical type at the Go level
    (not a "new named type"). spot.CreateOrderRequest{Side: types.SideTypeBuy}
    compiles without casts, even if internal code expects the common version.
  - Single source of truth: when values are changed/added (a new OKX OrderType),
    only types/enums.go needs updating.
  - Zero runtime cost: aliases do not create new typed values in the binary.

SPOT-SPECIFIC TYPES are in separate files of this package (create-order-request.go,
order-info.go, symbol-info.go, etc.).
*/

package types

import (
	commontypes "github.com/tonymontanov/go-okx/v2/types"
)

// SideType — order direction (buy/sell). See commontypes.SideType.
type SideType = commontypes.SideType

const (
	SideTypeBuy  = commontypes.SideTypeBuy
	SideTypeSell = commontypes.SideTypeSell
)

// OrderType — OKX order type. See commontypes.OrderType.
// Applicable for SPOT: market, limit, post_only, fok, ioc.
// OrderTypeOptimalLimitIOC is a SWAP-only constant and is absent from spot/types.
type OrderType = commontypes.OrderType

const (
	OrderTypeMarket   = commontypes.OrderTypeMarket
	OrderTypeLimit    = commontypes.OrderTypeLimit
	OrderTypePostOnly = commontypes.OrderTypePostOnly
	OrderTypeFOK      = commontypes.OrderTypeFOK
	OrderTypeIOC      = commontypes.OrderTypeIOC
)

// TimeInForceType — TIF (Binance-style). See commontypes.TimeInForceType.
type TimeInForceType = commontypes.TimeInForceType

const (
	TimeInForceTypeGTC = commontypes.TimeInForceTypeGTC
	TimeInForceTypeIOC = commontypes.TimeInForceTypeIOC
	TimeInForceTypeFOK = commontypes.TimeInForceTypeFOK
	TimeInForceTypeGTX = commontypes.TimeInForceTypeGTX
)

// TdMode — order margin mode. See commontypes.TdMode.
// For SPOT, TdModeCash (no leverage) is typically used. TdModeCross/Isolated
// are available for spot margin trading but are not used in the current SDK
// version (see doc.go).
type TdMode = commontypes.TdMode

const (
	TdModeCross    = commontypes.TdModeCross
	TdModeIsolated = commontypes.TdModeIsolated
	TdModeCash     = commontypes.TdModeCash
)

// InstType — OKX instrument type. The SPOT profile always returns InstTypeSpot.
type InstType = commontypes.InstType

const (
	InstTypeSpot    = commontypes.InstTypeSpot
	InstTypeSWAP    = commontypes.InstTypeSWAP
	InstTypeFutures = commontypes.InstTypeFutures
	InstTypeOption  = commontypes.InstTypeOption
)

// OrderState — order status. See commontypes.OrderState.
type OrderState = commontypes.OrderState

const (
	OrderStateLive            = commontypes.OrderStateLive
	OrderStatePartiallyFilled = commontypes.OrderStatePartiallyFilled
	OrderStateFilled          = commontypes.OrderStateFilled
	OrderStateCanceled        = commontypes.OrderStateCanceled
	OrderStateUnknown         = commontypes.OrderStateUnknown
)

// ParseOrderState — re-export.
var ParseOrderState = commontypes.ParseOrderState

// CancelAllAfterResult — response from POST /api/v5/trade/cancel-all-after.
// See commontypes.CancelAllAfterResult and types/cancel-all-after.go.
type CancelAllAfterResult = commontypes.CancelAllAfterResult

// Fill — a single order execution. See commontypes.Fill and types/fill.go.
type Fill = commontypes.Fill

// FillsQuery — parameters for fills retrieval (REST). See commontypes.FillsQuery.
type FillsQuery = commontypes.FillsQuery

// Shared data models — orderbook level/snapshot, candles, agg trades.
// Format is identical for spot and swap (REST/WS endpoints are shared), so
// we reuse via aliases from the common package.

// OrderBookLevel — order book level.
type OrderBookLevel = commontypes.OrderBookLevel

// OrderBookSnapshot — order book snapshot.
type OrderBookSnapshot = commontypes.OrderBookSnapshot

// Candle — a single candle.
type Candle = commontypes.Candle

// Candles — candle slice.
type Candles = commontypes.Candles

// Timeframe — candle request timeframe.
type Timeframe = commontypes.Timeframe

// Re-export of Timeframe constants — so a spot user can write
// types.Timeframe1m without a separate import of the common package.
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

// AggTrade — aggregated trade from the WS trades channel.
type AggTrade = commontypes.AggTrade

// QuotedSpreadUpdate — BBO from the bbo-tbt channel.
type QuotedSpreadUpdate = commontypes.QuotedSpreadUpdate

// Balance — unified-account state (shared between spot and swap; OKX returns
// a single balance per user).
type Balance = commontypes.Balance

// BalanceDetail — balance of a single currency within the unified-account.
type BalanceDetail = commontypes.BalanceDetail
