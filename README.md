# go-okx

Высокопроизводительный Go-SDK для биржи **OKX** (REST v5 + WebSocket v5), ориентированный на HFT/алготрейдинг.

Module path: `github.com/tonymontanov/go-okx/v2`

## Статус

| Модуль | Статус | Покрытие |
| --- | --- | --- |
| M0: каркас, типы, errors, logger | ✅ | — |
| M1: REST core (sign, transport, errors, rate-limit headers) | ✅ | unit-тесты подписи + codec |
| M1: swap.Trading (Create / Modify / Cancel + Batch*, CancelAll, CancelForgotten) | ✅ | contract-тесты |
| M1: swap.Account (Positions, OpenOrders, ClosePosition, SetLeverage, SetPositionMode) | ✅ | contract-тесты |
| M1: swap.MarketData (SymbolInfo, OrderBook snapshot, HistoricalCandles) | ✅ | contract-тесты |
| M2: orderbook.Engine (snapshot+delta+seqId+CRC32 checksum+resync) | ✅ | 9 unit-тестов |
| M3: internal/ws.Conn (connect/login/ping/reconnect+backoff/resubscribe/dispatch) | ✅ | 6 ws-тестов |
| M3: swap.Stream — WebSocket-подписки (Watch*) | ✅ | 9 методов: Orderbook/Spread/Mark/Index/Last/AggTrades/Position/OpenOrders/**Account** |
| M3: метрики (Counter/Add/Inc) — `okx_ws_*_total` | ✅ | — |
| M5: contract-тесты на JSON-фикстурах OKX | ✅ | 13 кейсов в `swap/contract_test.go` |
| M6: примеры | ✅ | `examples/orderbook-watcher`, `examples/simple-trade` |
| **A: Demo-mode** (`Config.Demo` → `x-simulated-trading: 1` + wspap WS) | ✅ | unit + интеграционный |
| **A: Account.GetBalance** (`/api/v5/account/balance`) | ✅ | contract-тесты |
| **A: Stream.WatchAccount** (private канал `account`) | ✅ | mock-WS integration |

В v1 поддержан **только профиль SWAP** (USD-M Perpetual, `instType=SWAP`).
Профиль SPOT — отдельная итерация.

## Зависимости

```
github.com/json-iterator/go      v1.1.12   // быстрый JSON в hot-path
github.com/shopspring/decimal    v1.4.0    // точные цены/количества
github.com/gorilla/websocket     v1.5.3    // WS-транспорт
```

## Структура

```
go-okx/
  client.go / config.go / errors.go / logger.go / metrics.go   // публичный корневой API
  swap/
    client.go, trading.go, account.go, market.go, stream.go
    contract_test.go                                  // contract-тесты на JSON OKX
    types/                                            // доменные структуры SWAP
  orderbook/
    engine.go                                         // snapshot+delta+CRC32 engine
  internal/
    auth/      — HMAC-SHA256 подпись OKX
    codec/     — jsoniter + helpers ParseDecimal/Int64
    okxerr/    — тип Error, категории, MapOKXCode/MapHTTPStatus
    okxlog/    — интерфейс Logger + Field/NoopLogger
    okxmet/    — интерфейс CounterFactory/Counter (Prometheus-shape)
    rest/      — низкоуровневый HTTP-клиент, обёртка {code, msg, data}
    ws/        — WS Conn: connect/login/reconnect+jitter/ping/resubscribe/dispatch
  examples/
    orderbook-watcher/  — public books → локальный стакан с CRC32 + best bid/ask
    simple-trade/       — place / modify / cancel лимитного ордера
```

## Архитектура (кратко)

Variant B из ТЗ §7: пользователю выдаётся «толстый» доменный клиент по
профилю (`swap.Client`), у которого есть саб-клиенты:

- `Trading()`    — Create/Modify/Cancel/Batch*/CancelAll/CancelForgotten.
- `Account()`    — **Balance**/Positions/OpenOrders/ClosePosition/SetLeverage/SetPositionMode.
- `MarketData()` — SymbolInfo/OrderBook/HistoricalCandles.
- `Stream()`     — Watch* (WebSocket; M3).

Низкоуровневые сервисы (`internal/rest`, `internal/ws`, `internal/auth`) скрыты
от пользователя и совместно используются всеми саб-клиентами.

Ошибки SDK — единый тип `*okx.Error` с полем `Kind` (Network/RateLimit/Auth/
InvalidRequest/Exchange/Unknown). Категория мапится из биржевого кода OKX
(`MapOKXCode`) или HTTP-статуса (`MapHTTPStatus`).

## Demo-режим (paper-trading)

Для проверки интеграции без реальных средств включите `Config.Demo`:

```go
var cfg okx.Config = okx.DefaultConfig()
cfg.Demo = true                    // ← всё, что нужно
cfg.APIKey = "<DEMO_API_KEY>"      // создайте отдельно: Profile → Demo Trading → API
cfg.SecretKey = "<DEMO_SECRET>"
cfg.Passphrase = "<DEMO_PASSPHRASE>"
```

Что произойдёт автоматически:

| Слой | Эффект |
|---|---|
| REST | ко всем запросам добавляется заголовок `x-simulated-trading: 1` (URL остаётся production — отличает только заголовок). |
| WebSocket | `wss://ws.okx.com:8443/...` заменяется на `wss://wspap.okx.com:8443/...` для public/private/business endpoint'ов. Если вы явно задали `cfg.WS.PublicURL` / `PrivateURL`, SDK его НЕ трогает. |
| Ключи | demo и prod ключи **не совместимы** — на бирже их выпускают отдельно. |

Контракт API между demo и prod на стороне OKX одинаковый — поэтому весь код
SDK работает без изменений; меняется только этот один флаг.

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
bal, err = sw.Account().GetBalance(ctx)               // все валюты
bal, err = sw.Account().GetBalance(ctx, "USDT", "BTC") // фильтр
```

Возвращает агрегированный `types.Balance` с top-level (TotalEquityUSD,
AdjustedEquityUSD, MarginRatio, NotionalUSD и т.д.) и `Details []BalanceDetail`
per-currency (Equity, AvailableEquity, FrozenBalance, UPL и т.п.). Тот же тип
используется в `Stream().WatchAccount` — на старте делаешь REST-snapshot, далее
живёшь на push'ах.

## Orderbook engine

```go
var eng *orderbook.Engine = orderbook.NewEngine("BTC-USDT-SWAP", 400, 25)

// 1. снапшот (REST или WS)
eng.ApplySnapshot(orderbook.Snapshot{ /* ... */ })

// 2. поток дельт из WS
var res orderbook.ApplyResult = eng.ApplyUpdate(orderbook.Update{ /* ... */ })
if res.Gap != orderbook.GapNone {
    // resync: запросить снапшот заново
}

var bids, asks = eng.TopLevels(10)
```

Подробнее об алгоритме — см. `orderbook/engine.go`.

## WebSocket-стримы

Все `Watch*` методы используют один public-conn и один private-conn на весь
`*swap.Client`. Reconnect, backoff с jitter, resubscribe и login (для private)
прозрачны для пользователя.

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

Доступные подписки:

| Метод | OKX-канал | Назначение |
| --- | --- | --- |
| `WatchOrderbook` | `books` | локальный стакан с CRC32-валидацией |
| `WatchSpread` | `bbo-tbt` | best bid/ask с размерами |
| `WatchMarkPrice` | `mark-price` | mark price (float64, ts) |
| `WatchIndexPrice` | `index-tickers` | index price |
| `WatchLastPrice` | `trades` | последняя цена сделки |
| `WatchAggTrades` | `trades` | сделки целиком (price+size+side+ts) |
| `WatchPosition` | `positions` (private) | обновления позиции |
| `WatchOpenOrders` | `orders` (private) | обновления ордеров |
| `WatchAccount` | `account` (private) | полный снимок баланса unified-account |

Счётчики (через `okx.Config.Metrics`):

```
okx_ws_messages_received_total
okx_ws_messages_dropped_total
okx_ws_reconnects_total
okx_ws_subscriptions_total
okx_ws_ping_failed_total
```

По умолчанию — `NoopMetrics()`; подключите свою фабрику для интеграции с
Prometheus/любой другой системой.

## Примеры

| Пример | Что делает | Ключи | OKX_ALLOW_LIVE |
|---|---|---|---|
| `examples/market-data` | symbol-info, order-book snapshot, candles | нет | нет |
| `examples/public-streams` | bbo-tbt, mark-price, index, last, agg trades | нет | нет |
| `examples/orderbook-watcher` | public books + локальный стакан с CRC32 | нет | нет |
| `examples/account-info` | symbol-info, position, open orders | да | нет |
| `examples/simple-trade` | place → modify → cancel лимитного ордера далеко от рынка | да | **да** |
| `examples/inventory-tracker` | private streams + market buy + close position (одноразовый смоук) | да | **да** |
| `examples/inventory-monitor` | бесконечный мониторинг позиции и ордеров (до Ctrl-C) | да | нет |

### Как запускать

Один раз создай `.env` из шаблона и пропиши ключи:

```bash
cp .env.example .env
# открой .env, заполни OKX_API_KEY / OKX_SECRET_KEY / OKX_PASSPHRASE
# для торговых примеров поставь OKX_ALLOW_LIVE=1
```

Любой пример запускается через wrapper, который читает `.env`:

```bash
./scripts/run.sh ./examples/market-data
./scripts/run.sh ./examples/public-streams
./scripts/run.sh ./examples/account-info
./scripts/run.sh ./examples/simple-trade
./scripts/run.sh ./examples/inventory-tracker
```

Без `.env` тоже можно — пример без ключей просто игнорирует пустые переменные:

```bash
go run ./examples/market-data
```

### Дополнительные переменные

| Переменная | Где используется | По умолчанию |
|---|---|---|
| `OKX_INSTRUMENT` | account-info, market-data, public-streams, inventory-tracker | `BTC-USDT-SWAP` |
| `OKX_SIZE` | inventory-tracker | `1` (контракт) |
| `OKX_HOLD_SECONDS` | inventory-tracker | `5` секунд между BUY и close |

Пример «дешёвого» live-теста (~1-2 USDT на спред + комиссии):

```bash
OKX_INSTRUMENT=DOGE-USDT-SWAP OKX_SIZE=1 \
  ./scripts/run.sh ./examples/inventory-tracker
```

## Codestyle

- Файловые заголовки на русском (как в `market-making-desk-core`).
- Явное объявление переменных через `var name type = ...`.
- `camelCase` для локальных идентификаторов, `PascalCase` для экспортируемых.
- `jsoniter` через `internal/codec` для hot-path; `encoding/json` напрямую не используется.
- Все методы принимают `context.Context` первым параметром; `context.Background()`
  внутри методов с `ctx` запрещён.

## Лицензия

См. `LICENSE`.
