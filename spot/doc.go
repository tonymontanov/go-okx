/*
Package spot — SPOT profile of the OKX v5 SDK.

Purpose: trading and market-data streams for OKX spot instruments
(instType=SPOT, instID of the form "BTC-USDT"). Architecturally mirrors the
github.com/tonymontanov/go-okx/v2/swap package — same sub-clients, same transport,
same WS callback style.

# Registration

To use spot via the root okx.Client, a blank-import is sufficient:

	import (
	    okx "github.com/tonymontanov/go-okx/v2"
	    "github.com/tonymontanov/go-okx/v2/spot"
	    _ "github.com/tonymontanov/go-okx/v2/spot" // registers the factory in okx
	)

	client, _ := okx.NewClient(cfg)
	spotClient := client.Spot().(*spot.Client)

# Differences from swap

  - No concept of "contract": all sizes (sz) are in the BASE currency. No
    base↔contracts conversion is needed.
  - No hedge mode (PosSide, ReduceOnly): on cash spot you can only buy and sell;
    shorting is not possible.
  - SymbolInfo does not contain CtVal/CtMult.
  - tdMode defaults to "cash" (for cash trading); for spot margin, set TdMode
    explicitly (TdModeCross/TdModeIsolated).
  - For a market BUY, OKX interprets sz in the QUOTE currency by default
    (USDT for BTC-USDT). To buy exactly N units of the base currency, set
    CreateOrderRequest.TgtCcy = TgtCcyBase.

# Shared with swap

  - REST/WS transport (internal/rest, internal/ws) — reused as-is.
  - Unified-account: a single balance endpoint /api/v5/account/balance covers
    spot + swap. The Balance type is reused via an alias (spot/types/aliases.go).
  - Protocol enums: SideType, OrderType, TimeInForceType, InstType,
    OrderState, OrderBookLevel, AggTrade, Candle, Timeframe, QuotedSpreadUpdate —
    also re-exported as aliases of swap/types so that code is compatible without casts.

# Margin trading on spot

The current SDK version supports cash mode only. The TdMode and Ccy fields in
CreateOrderRequest are declared and passed to REST as-is, but there are NO separate
helper methods for margin short/borrow. These will be added in future versions based
on real demand.
*/
package spot
