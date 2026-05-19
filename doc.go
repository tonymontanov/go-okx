/*
FILE: doc.go

DESCRIPTION:
Package okx is the root SDK package for the OKX exchange (REST v5 + WebSocket v5),
designed for high-frequency trading. It hosts entities common to all profiles:
Config, Logger, ErrorKind/Error, rate-limit events, and the top-level Client
that provides sub-clients for product sections.

CORE IDEA:
The SDK is built on a domain-based architecture: domain sub-clients
(Trading/Account/MarketData/Stream) are exposed to the user, while low-level
services (request signing, HTTP pool, WS pool, response parsing) are hidden in
internal packages. This simultaneously:
  - keeps the public API minimal;
  - avoids pulling half of the internals into third-party projects via `go doc`.

PRODUCT SECTIONS:
  v2.x (released):
    - swap   (USD-M Perpetual, instType=SWAP)
    - spot   (instType=SPOT)

  v2.5-dev (in progress):
    - foundation: WS request/reply framing, extended common types;
    - critical HFT: WS Order API, cancel-all-after, extended WS books
      channels (books5/books-l2-tbt/books50-l2-tbt), fills (REST+WS);
    - perp-analytics: ticker/mark-price/funding/open-interest/liquidations
      (REST+WS) + system time/status + orders-history;
    - account/risk: account/config, max-size, leverage-info, risk-state,
      bills, set-account-level/greeks;
    - capital management: asset/ (funding account), subaccount/;
    - additional sections: margin (instType=MARGIN), futures (instType=FUTURES),
      options (instType=OPTION) — each as a top-level package.

COMMON PROTOCOL LAYER:
All shared enums (SideType, OrderType, TimeInForce, TdMode, InstType,
OrderState) and neutral structs (OrderBook*, Candle, AggTrade, Balance,
Ticker, FundingRate, MarkPrice, etc.) live in the package
`github.com/tonymontanov/go-okx/v2/types`. Profile packages (swap/types,
spot/types, etc.) alias from there as needed. This eliminates any cross-import
between product sections and allows writing cross-section code without import
cycles.

DEPENDENCIES:
- github.com/json-iterator/go     — fast JSON for hot-path;
- github.com/gorilla/websocket    — WS connections;
- github.com/shopspring/decimal   — precise numbers for prices/quantities.
*/
package okx
