/*
FILE: swap/types/enums.go

DESCRIPTION:
OKX SWAP profile enums. Most values are shared with other profiles
(spot etc.) — re-exported via aliases from the package
github.com/tonymontanov/go-okx/v2/types (see types/enums.go).

Only SWAP-specific enums and constants live here:
  - PosSide        — long/short/net (position sides in hedge mode).
  - PositionMode   — net_mode/long_short_mode (account mode).
  - OrderTypeOptimalLimitIOC — swap-only constant extending the common OrderType
                               (used by ClosePosition for market close).

BACKWARDS COMPATIBILITY:
All types previously accessible through swap/types continue to work without
changes on the user side. The type-alias `type X = types.X` at the Go level
is equivalent to the original type — no casts or additional imports are needed.
*/

package types

import (
	commontypes "github.com/tonymontanov/go-okx/v2/types"
)

// SideType — order direction. See commontypes.SideType.
type SideType = commontypes.SideType

const (
	// SideTypeBuy — buy.
	SideTypeBuy = commontypes.SideTypeBuy
	// SideTypeSell — sell.
	SideTypeSell = commontypes.SideTypeSell
)

// PosSide — position side in SWAP. Used in hedge mode (long_short_mode).
// In net_mode (SDK default) always set to "net".
//
// SWAP-ONLY: cash spot has no position concept; this type is absent in spot/types.
type PosSide string

const (
	// PosSideLong — long side (hedge mode).
	PosSideLong PosSide = "long"
	// PosSideShort — short side (hedge mode).
	PosSideShort PosSide = "short"
	// PosSideNet — net mode (one position per instrument).
	PosSideNet PosSide = "net"
)

// OrderType — OKX order type notation. See commontypes.OrderType.
type OrderType = commontypes.OrderType

const (
	// OrderTypeMarket — market order.
	OrderTypeMarket = commontypes.OrderTypeMarket
	// OrderTypeLimit — limit (GTC).
	OrderTypeLimit = commontypes.OrderTypeLimit
	// OrderTypePostOnly — limit post-only (equivalent to TIF=GTX on Binance).
	OrderTypePostOnly = commontypes.OrderTypePostOnly
	// OrderTypeFOK — fill-or-kill.
	OrderTypeFOK = commontypes.OrderTypeFOK
	// OrderTypeIOC — immediate-or-cancel.
	OrderTypeIOC = commontypes.OrderTypeIOC
	// OrderTypeOptimalLimitIOC — SWAP-only market order (fills at best price + IOC,
	// used by ClosePosition for market close). OKX returns an error for SPOT,
	// so this constant lives here rather than in the common package.
	OrderTypeOptimalLimitIOC OrderType = "optimal_limit_ioc"
)

// TimeInForceType — TIF in core/types notation (Binance-style). See commontypes.
type TimeInForceType = commontypes.TimeInForceType

const (
	// TimeInForceTypeGTC — Good Till Cancel (limit).
	TimeInForceTypeGTC = commontypes.TimeInForceTypeGTC
	// TimeInForceTypeIOC — Immediate or Cancel.
	TimeInForceTypeIOC = commontypes.TimeInForceTypeIOC
	// TimeInForceTypeFOK — Fill or Kill.
	TimeInForceTypeFOK = commontypes.TimeInForceTypeFOK
	// TimeInForceTypeGTX — Post Only.
	TimeInForceTypeGTX = commontypes.TimeInForceTypeGTX
)

// TdMode — order/position margin mode in OKX. See commontypes.TdMode.
type TdMode = commontypes.TdMode

const (
	// TdModeCross — cross-margin (USD-M SWAP default).
	TdModeCross = commontypes.TdModeCross
	// TdModeIsolated — isolated margin.
	TdModeIsolated = commontypes.TdModeIsolated
	// TdModeCash — for the SPOT profile; not used in SWAP, but the constant is
	// available for cases where swap code passes a value obtained from the
	// common layer.
	TdModeCash = commontypes.TdModeCash
)

// InstType — OKX instrument type. The SWAP profile always returns InstTypeSWAP.
// See commontypes.InstType.
type InstType = commontypes.InstType

const (
	// InstTypeSpot — spot instrument.
	InstTypeSpot = commontypes.InstTypeSpot
	// InstTypeSWAP — USD-M Perpetual SWAP.
	InstTypeSWAP = commontypes.InstTypeSWAP
	// InstTypeFutures — expiring futures.
	InstTypeFutures = commontypes.InstTypeFutures
	// InstTypeOption — option.
	InstTypeOption = commontypes.InstTypeOption
)

// PositionMode — account position mode (see SetPositionMode).
//
// SWAP-ONLY: cash spot has no position concept; the mode is not configurable there.
type PositionMode string

const (
	// PositionModeNet — net mode (one position per instrument). SDK default.
	PositionModeNet PositionMode = "net_mode"
	// PositionModeLongShort — hedge mode (long + short simultaneously).
	PositionModeLongShort PositionMode = "long_short_mode"
)

// OrderState — minimal order status enum. See commontypes.OrderState.
type OrderState = commontypes.OrderState

const (
	// OrderStateLive — active, waiting for execution.
	OrderStateLive = commontypes.OrderStateLive
	// OrderStatePartiallyFilled — partially filled, remainder active.
	OrderStatePartiallyFilled = commontypes.OrderStatePartiallyFilled
	// OrderStateFilled — fully filled.
	OrderStateFilled = commontypes.OrderStateFilled
	// OrderStateCanceled — canceled.
	OrderStateCanceled = commontypes.OrderStateCanceled
	// OrderStateUnknown — status could not be parsed (defensive fallback).
	OrderStateUnknown = commontypes.OrderStateUnknown
)

// ParseOrderState — re-export of the function from the common package. Call as
// swaptypes.ParseOrderState(s) — behaviour is unchanged.
var ParseOrderState = commontypes.ParseOrderState

// CancelAllAfterResult — response for POST /api/v5/trade/cancel-all-after.
// See commontypes.CancelAllAfterResult and types/cancel-all-after.go.
type CancelAllAfterResult = commontypes.CancelAllAfterResult

// Fill — a single order execution. See commontypes.Fill and types/fill.go.
type Fill = commontypes.Fill

// FillsQuery — fill query parameters (REST). See commontypes.FillsQuery.
type FillsQuery = commontypes.FillsQuery

// AccountRateLimitInfo — response of GET /api/v5/account/rate-limit.
// See commontypes.AccountRateLimitInfo and types/account-rate-limit.go.
type AccountRateLimitInfo = commontypes.AccountRateLimitInfo
