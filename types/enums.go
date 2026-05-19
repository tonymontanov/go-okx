/*
FILE: types/enums.go

DESCRIPTION:
OKX v5 protocol enums shared by spot, swap, and future profiles.
All values are string-typed enums that exactly match the OKX v5 protocol
(https://www.okx.com/docs-v5/en/). This allows:
  - direct serialization/deserialization without mapping;
  - using values directly in URLs/JSON;
  - comparison via `==` without allocations.

EXTRACTED FROM swap/types/enums.go:
  - SideType        — buy/sell.
  - OrderType       — shared values (market/limit/post_only/fok/ioc).
                      OrderTypeOptimalLimitIOC stays in swap/types as
                      profile-specific (used only for market ClosePosition).
  - TimeInForceType — GTC/IOC/FOK/PostOnly (Binance-style; mapped to
                      OKX OrderType in the SDK).
  - TdMode          — cross/isolated/cash. All three values are protocol constants;
                      cash — spot default, cross/isolated — swap default.
  - InstType        — SPOT/SWAP/FUTURES/OPTION.
  - OrderState      — live/partially_filled/filled/canceled/unknown + ParseOrderState.

REMAINS IN swap/types/enums.go (profile-specific):
  - PosSide        — long/short/net (position sides in SWAP hedge mode).
  - PositionMode   — net_mode/long_short_mode (account mode for SWAP).
  - OrderTypeOptimalLimitIOC — swap-only constant extending the common OrderType.
*/

package types

// SideType — order direction. Shared across all OKX profiles.
type SideType string

const (
	// SideTypeBuy — buy.
	SideTypeBuy SideType = "buy"
	// SideTypeSell — sell.
	SideTypeSell SideType = "sell"
)

// OrderType — OKX order type notation. POST_ONLY / FOK / IOC are expressed
// as a separate OrderType rather than TIF — this is specific to their REST protocol.
//
// This enum carries SHARED values supported by spot and swap. Profile-specific
// values (e.g. OrderTypeOptimalLimitIOC in swap) are declared in their own
// package as constants of the same OrderType.
type OrderType string

const (
	// OrderTypeMarket — market order.
	OrderTypeMarket OrderType = "market"
	// OrderTypeLimit — limit (GTC).
	OrderTypeLimit OrderType = "limit"
	// OrderTypePostOnly — limit post-only (equivalent to TIF=GTX on Binance).
	OrderTypePostOnly OrderType = "post_only"
	// OrderTypeFOK — fill-or-kill.
	OrderTypeFOK OrderType = "fok"
	// OrderTypeIOC — immediate-or-cancel.
	OrderTypeIOC OrderType = "ioc"
)

// TimeInForceType — TIF in core/types notation (Binance-style). Mapped to
// the corresponding OKX OrderType when sending to the exchange.
type TimeInForceType string

const (
	// TimeInForceTypeGTC — Good Till Cancel (limit).
	TimeInForceTypeGTC TimeInForceType = "GTC"
	// TimeInForceTypeIOC — Immediate or Cancel.
	TimeInForceTypeIOC TimeInForceType = "IOC"
	// TimeInForceTypeFOK — Fill or Kill.
	TimeInForceTypeFOK TimeInForceType = "FOK"
	// TimeInForceTypeGTX — Post Only.
	TimeInForceTypeGTX TimeInForceType = "GTX"
)

// TdMode — margin mode for an order/position in OKX.
// All three values are OKX protocol constants:
//   - cash      used by default for SPOT;
//   - cross     default for SWAP (cross-margin);
//   - isolated  for isolated-margin positions.
type TdMode string

const (
	// TdModeCross — cross-margin.
	TdModeCross TdMode = "cross"
	// TdModeIsolated — isolated margin.
	TdModeIsolated TdMode = "isolated"
	// TdModeCash — cash trading (no leverage); SPOT default.
	TdModeCash TdMode = "cash"
)

// InstType — OKX instrument type.
type InstType string

const (
	// InstTypeSpot — spot instrument.
	InstTypeSpot InstType = "SPOT"
	// InstTypeSWAP — USD-M Perpetual SWAP.
	InstTypeSWAP InstType = "SWAP"
	// InstTypeFutures — expiring futures.
	InstTypeFutures InstType = "FUTURES"
	// InstTypeOption — option.
	InstTypeOption InstType = "OPTION"
)

// OrderState — minimal order status enum. Introduced as a compromise between
// "don't introduce a general status model" and the real need to filter
// live/filled/canceled.
type OrderState string

const (
	// OrderStateLive — active, waiting for execution.
	OrderStateLive OrderState = "live"
	// OrderStatePartiallyFilled — partially filled, remainder active.
	OrderStatePartiallyFilled OrderState = "partially_filled"
	// OrderStateFilled — fully filled.
	OrderStateFilled OrderState = "filled"
	// OrderStateCanceled — canceled.
	OrderStateCanceled OrderState = "canceled"
	// OrderStateUnknown — status could not be parsed (defensive fallback).
	OrderStateUnknown OrderState = "unknown"
)

// ParseOrderState parses an OKX order status string into a typed enum.
// OKX possible values: live, partially_filled, filled, canceled, mmp_canceled.
// Anything unknown → OrderStateUnknown (log at the call site for diagnostics).
func ParseOrderState(s string) OrderState {
	switch s {
	case "live":
		return OrderStateLive
	case "partially_filled":
		return OrderStatePartiallyFilled
	case "filled":
		return OrderStateFilled
	case "canceled", "mmp_canceled":
		return OrderStateCanceled
	default:
		return OrderStateUnknown
	}
}
