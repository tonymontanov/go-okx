# go-okx

High-performance Go SDK for the **OKX** exchange (REST v5 + WebSocket v5), targeting HFT/algorithmic trading.

Module path: `github.com/tonymontanov/go-okx/v2`

## Status

| Module | Status | Coverage |
| --- | --- | --- |
| M0: scaffolding, types, errors, logger | ✅ | — |
| M1: REST core (sign, transport, errors, rate-limit headers) | ✅ | unit tests for signing + codec |
| M1: swap.Trading (Create / Modify / Cancel + Batch*, CancelAll, CancelForgotten) | ✅ | contract tests |
| M1: swap.Account (Positions, OpenOrders, ClosePosition, SetLeverage, SetPositionMode) | ✅ | contract tests |
| M1: swap.MarketData (SymbolInfo, OrderBook snapshot, HistoricalCandles) | ✅ | contract tests |
| M2: orderbook.Engine (snapshot+delta+seqId+CRC32 checksum+resync) | ✅ | 9 unit tests |
| M3: internal/ws.Conn (connect/login/ping/reconnect+backoff/resubscribe/dispatch) | ✅ | 6 ws tests |
| M3: swap.Stream — WebSocket subscriptions (Watch*) | ✅ | 9 methods: Orderbook/Spread/Mark/Index/Last/AggTrades/Position/OpenOrders/**Account** |
| M3: metrics (Counter/Add/Inc) — `okx_ws_*_total` | ✅ | — |
| M5: contract tests on OKX JSON fixtures | ✅ | 13 cases in `swap/contract_test.go` |
| M6: examples | ✅ | `examples/orderbook-watcher`, `examples/simple-trade` |
| **A: Demo mode** (`Config.Demo` → `x-simulated-trading: 1` + wspap WS) | ✅ | unit + integration |
| **A: Account.GetBalance** (`/api/v5/account/balance`) | ✅ | contract tests |
| **A: Stream.WatchAccount** (private channel `account`) | ✅ | mock-WS integration |

v1 supports the **SWAP profile only** (USD-M Perpetual, `instType=SWAP`).
SPOT profile is a separate iteration.

## Dependencies

```
github.com/json-iterator/go      v1.1.12   // fast JSON in hot-path
github.com/shopspring/decimal    v1.4.0    // precise prices/quantities
github.com/gorilla/websocket     v1.5.3    // WS transport
```

## Structure

```
go-okx/
  client.go / config.go / errors.go / logger.go / metrics.go   // public root API
  swap/
    client.go, trading.go, account.go, market.go, stream.go
    contract_test.go                                  // contract tests on OKX JSON
    types/                                            // SWAP domain structs
  orderbook/
    engine.go                                         // snapshot+delta+CRC32 engine
  internal/
    auth/      — HMAC-SHA256 signing for OKX
    codec/     — jsoniter + helpers ParseDecimal/Int64
    okxerr/    — Error type, categories, MapOKXCode/MapHTTPStatus
    okxlog/    — Logger interface + Field/NoopLogger
    okxmet/    — CounterFactory/Counter interface (Prometheus-shape)
    rest/      — low-level HTTP client, {code, msg, data} wrapper
    ws/        — WS Conn: connect/login/reconnect+jitter/ping/resubscribe/dispatch
  examples/
    orderbook-watcher/  — public books → local order book with CRC32 + best bid/ask
    simple-trade/       — place / modify / cancel a limit order
```

## Architecture (brief)

Variant B: the user receives a "fat" domain client per profile (`swap.Client`) with sub-clients:

- `Trading()`    — Create/Modify/Cancel/Batch*/CancelAll/CancelForgotten.
- `Account()`    — **Balance**/Positions/OpenOrders/ClosePosition/SetLeverage/SetPositionMode.
- `MarketData()` — SymbolInfo/OrderBook/HistoricalCandles.
- `Stream()`     — Watch* (WebSocket; M3).

Low-level services (`internal/rest`, `internal/ws`, `internal/auth`) are hidden from the user
and shared across all sub-clients.

SDK errors use a single type `*okx.Error` with a `Kind` field (Network/RateLimit/Auth/
InvalidRequest/Exchange/Unknown). The category is mapped from the OKX exchange code
(`MapOKXCode`) or HTTP status (`MapHTTPStatus`).

## Demo mode (paper trading)

To test integration without real funds, enable `Config.Demo`:

```go
var cfg okx.Config = okx.DefaultConfig()
cfg.Demo = true                    // ← that's all
cfg.APIKey = "<DEMO_API_KEY>"      // create separately: Profile → Demo Trading → API
cfg.SecretKey = "<DEMO_SECRET>"
cfg.Passphrase = "<DEMO_PASSPHRASE>"
```

What happens automatically:

| Layer | Effect |
|---|---|
| REST | The header `x-simulated-trading: 1` is added to every request (URL stays production — only the header differs). |
| WebSocket | `wss://ws.okx.com:8443/...` is replaced with `wss://wspap.okx.com:8443/...` for public/private/business endpoints. If `cfg.WS.PublicURL` / `PrivateURL` are set explicitly, the SDK leaves them unchanged. |
| Keys | Demo and prod keys are **not interchangeable** — they are issued separately on the exchange. |

The API contract between demo and prod on the OKX side is identical — all SDK code works
without changes; only this one flag is toggled.

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/shopspring/decimal"
    okx "github.com/tonymontanov/go-okx/v2"
    "github.com/tonymontanov/go-okx/v2/swap"
    swaptypes "github.com/tonymontanov/go-okx/v2/swap/types"
)

func main() {
    var cfg okx.Config = okx.DefaultConfig()
    cfg.APIKey = "..."
    cfg.SecretKey = "..."
    cfg.Passphrase = "..."

    var client *okx.Client
    var err error
    client, err = okx.NewClient(cfg)
    if err != nil {
        panic(err)
    }
    defer client.Close()

    var sw *swap.Client = swap.NewClient(client)

    var ctx context.Context
    var cancel context.CancelFunc
    ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    var info swaptypes.OrderInfo
    info, err = sw.Trading().CreateOrder(ctx, swaptypes.CreateOrderRequest{
        InstID:        "BTC-USDT-SWAP",
        Side:          swaptypes.SideTypeBuy,
        OrderType:     swaptypes.OrderTypeLimit,
        Size:          decimal.NewFromInt(1),
        Price:         decimal.RequireFromString("60000"),
        ClientOrderID: "myclient001",
    })
    if err != nil {
        switch {
        case okx.IsRateLimit(err):
            // backoff
        case okx.IsAuth(err):
            // terminate
        default:
            // log
        }
        return
    }
    fmt.Printf("ordId=%s state=%s\n", info.OrderID, info.State)
}
```

## Account snapshot (REST)

```go
var bal swaptypes.Balance
bal, err = sw.Account().GetBalance(ctx)               // all currencies
bal, err = sw.Account().GetBalance(ctx, "USDT", "BTC") // filtered
```

Returns an aggregated `types.Balance` with top-level fields (TotalEquityUSD,
AdjustedEquityUSD, MarginRatio, NotionalUSD, etc.) and `Details []BalanceDetail`
per currency (Equity, AvailableEquity, FrozenBalance, UPL, etc.). The same type
is used in `Stream().WatchAccount` — take a REST snapshot on startup, then
rely on pushes.

## Orderbook engine

```go
var eng *orderbook.Engine = orderbook.NewEngine("BTC-USDT-SWAP", 400, 25)

// 1. snapshot (REST or WS)
eng.ApplySnapshot(orderbook.Snapshot{ /* ... */ })

// 2. delta stream from WS
var res orderbook.ApplyResult = eng.ApplyUpdate(orderbook.Update{ /* ... */ })
if res.Gap != orderbook.GapNone {
    // resync: request a new snapshot
}

var bids, asks = eng.TopLevels(10)
```

For details on the algorithm see `orderbook/engine.go`.

## WebSocket streams

All `Watch*` methods share a single public-conn and a single private-conn per
`*swap.Client`. Reconnect, backoff with jitter, resubscribe, and login (for private)
are transparent to the caller.

```go
err := sw.Stream().WatchOrderbook(ctx, "BTC-USDT-SWAP", 5,
    func(snap swaptypes.OrderBookSnapshot) {
        fmt.Println(snap.Bids[0].Price, snap.Asks[0].Price)
    },
    func(streamErr error) {
        log.Printf("stream error: %v", streamErr)
    },
)
```

Available subscriptions:

| Method | OKX channel | Purpose |
| --- | --- | --- |
| `WatchOrderbook` | `books` | local order book with CRC32 validation |
| `WatchSpread` | `bbo-tbt` | best bid/ask with sizes |
| `WatchMarkPrice` | `mark-price` | mark price (float64, ts) |
| `WatchIndexPrice` | `index-tickers` | index price |
| `WatchLastPrice` | `trades` | last trade price |
| `WatchAggTrades` | `trades` | full trades (price+size+side+ts) |
| `WatchPosition` | `positions` (private) | position updates |
| `WatchOpenOrders` | `orders` (private) | order updates |
| `WatchAccount` | `account` (private) | full unified-account balance snapshot |

Counters (via `okx.Config.Metrics`):

```
okx_ws_messages_received_total
okx_ws_messages_dropped_total
okx_ws_reconnects_total
okx_ws_subscriptions_total
okx_ws_ping_failed_total
```

Default is `NoopMetrics()`; plug in your own factory to integrate with
Prometheus or any other system.

## Examples

| Example | What it does | Keys | OKX_ALLOW_LIVE |
|---|---|---|---|
| `examples/market-data` | symbol-info, order-book snapshot, candles | no | no |
| `examples/public-streams` | bbo-tbt, mark-price, index, last, agg trades | no | no |
| `examples/orderbook-watcher` | public books + local order book with CRC32 | no | no |
| `examples/account-info` | symbol-info, position, open orders | yes | no |
| `examples/simple-trade` | place → modify → cancel a limit order far from market | yes | **yes** |
| `examples/inventory-tracker` | private streams + market buy + close position (one-shot smoke) | yes | **yes** |
| `examples/inventory-monitor` | continuous position and order monitoring (until Ctrl-C) | yes | no |

### How to run

Create `.env` from the template once and fill in the keys:

```bash
cp .env.example .env
# open .env, fill in OKX_API_KEY / OKX_SECRET_KEY / OKX_PASSPHRASE
# set OKX_ALLOW_LIVE=1 for trading examples
```

Any example can be run via the wrapper that reads `.env`:

```bash
./scripts/run.sh ./examples/market-data
./scripts/run.sh ./examples/public-streams
./scripts/run.sh ./examples/account-info
./scripts/run.sh ./examples/simple-trade
./scripts/run.sh ./examples/inventory-tracker
```

Without `.env` it also works — examples without keys simply ignore empty variables:

```bash
go run ./examples/market-data
```

### Additional variables

| Variable | Used in | Default |
|---|---|---|
| `OKX_INSTRUMENT` | account-info, market-data, public-streams, inventory-tracker | `BTC-USDT-SWAP` |
| `OKX_SIZE` | inventory-tracker | `1` (contract) |
| `OKX_HOLD_SECONDS` | inventory-tracker | `5` seconds between BUY and close |

Example of a "cheap" live test (~1-2 USDT on spread + fees):

```bash
OKX_INSTRUMENT=DOGE-USDT-SWAP OKX_SIZE=1 \
  ./scripts/run.sh ./examples/inventory-tracker
```

## Code style

- File headers in English.
- Explicit variable declarations via `var name type = ...`.
- `camelCase` for local identifiers, `PascalCase` for exported ones.
- `jsoniter` via `internal/codec` for hot-path; `encoding/json` is not used directly.
- All methods take `context.Context` as the first parameter; `context.Background()`
  inside methods that already have `ctx` is forbidden.

## License

See `LICENSE`.
