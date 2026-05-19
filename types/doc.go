/*
Package types — protocol-shared OKX v5 types used by all profiles
(spot, swap, and futures/options in the future).

WHY A SEPARATE PACKAGE:
Previously "common" enums and data structures (SideType, OrderType, OrderBookLevel,
Balance, ...) lived in swap/types, and spot/types referenced them via aliases. This
created a logically incorrect dependency spot → swap: the spot profile must not
know about the existence of swap; both should be equal consumers of the protocol layer.

This package is a dedicated neutral layer:

	github.com/tonymontanov/go-okx/v2/types/  ← shared protocol layer (this package)
	    ↑                          ↑
	    │                          │
	swap/types/                spot/types/   ← profile packages, aliases + own types

WHAT LIVES HERE:
  - OKX protocol enums common to all instTypes: SideType, OrderType (shared
    values), TimeInForceType, TdMode, InstType, OrderState + ParseOrderState.
  - WS/REST structs with an identical format across all profiles: OrderBookLevel,
    OrderBookSnapshot, Candle/Candles, Timeframe, AggTrade, QuotedSpreadUpdate.
  - Unified-account model: Balance, BalanceDetail (OKX returns ONE balance per
    user for all profiles).

WHAT DOES NOT LIVE HERE:
  - Profile-specific request types: CreateOrderRequest, ModifyOrderRequest,
    CancelOrderRequest, OrderInfo, SymbolInfo. spot and swap differ in their
    field sets (PosSide/ReduceOnly exist only in swap; TgtCcy — only in spot;
    SymbolInfo.CtVal — only in swap, etc.). See swap/types/* and spot/types/*.
  - Enums and constants specific to one profile: PosSide, PositionMode
    (swap-only, hedge mode); OrderTypeOptimalLimitIOC (swap-only order type).
    They live in swap/types and extend the common OrderType.

BACKWARDS COMPATIBILITY:
Existing users continue importing swap/types and spot/types and accessing types
through them — both packages type-alias to this one. A direct import of
github.com/tonymontanov/go-okx/v2/types is not required.

CODE STYLE:
This package follows the same rules as the rest of the SDK:
  - file names in kebab-case;
  - one type per file where it makes sense;
  - struct fields with decimal.Decimal, no epsilon comparisons;
  - file header annotation at the top of each file.
*/
package types
