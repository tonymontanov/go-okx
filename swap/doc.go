/*
FILE: swap/doc.go

DESCRIPTION:
Package swap implements the OKX SWAP profile (USD-M Perpetual). This is a "fat"
domain client following the Variant B architecture: the user receives four
sub-clients — Trading, Account, MarketData, Stream — each with their own
methods in idiomatic Go style.

PUBLIC ENTRY POINTS:
  - swap.NewClient(parent *okx.Client) *Client    — standard path;
  - parent.Swap().(*swap.Client)                  — same thing lazily, without
                                                    duplicating the parent argument.

SUB-CLIENTS:
  - (*Client).Trading()    : CreateOrder, ModifyOrder, CancelOrder, batch variants, CancelAll, CancelForgotten.
  - (*Client).Account()    : GetPosition/GetOpenOrders/ClosePosition/SetLeverage/SetPositionMode.
  - (*Client).MarketData() : GetSymbolInfo/GetOrderBook/GetHistoricalCandles.
  - (*Client).Stream()     : Watch* (WebSocket subscriptions).

TYPES:
All domain structs (CreateOrderRequest, OrderInfo, PositionInfo, …) live in the
swap/types sub-package and are used by both the SDK sub-clients and the desk adapter.

v1 supports SWAP only (instType=SWAP). The SPOT profile is implemented
separately (package spot).
*/
package swap
