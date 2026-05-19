# Technical Specification: Single-Exchange SDK

**Version:** 1.0  
**Date:** 2025-02  
**Context:** A dedicated Go SDK for a single exchange; unification lives in the desk code. API style reference: adshao/go-binance v2.8.9.

---

## 1) Overview and goals

**Goal:**  
Build a high-performance Go SDK for **one specific exchange** (hereafter — *Exchange*) targeting HFT/algorithmic trading, which:

- covers the REST + WebSocket API of the given Exchange;
- provides a convenient, idiomatic Go API (in the spirit of adshao/go-binance);
- ensures correctness of the order book and order/position state;
- wraps cleanly into the existing unified desk interface (`ExchangeConnector`), but is **not itself a multi-exchange abstraction**.

---

## 2) Glossary

- **SDK**: Go library for working with a single Exchange (REST + WS).
- **REST Client**: SDK component that implements HTTP calls to the Exchange REST API.
- **WS Client**: SDK component that implements connections and subscriptions to the WebSocket API.
- **Orderbook engine**: SDK component that maintains a consistent order book (snapshot + delta + seq + gap detection + resync).
- **Order lifecycle**: the complete state of an order from creation to terminal status.
- **ClientOrderId**: client-side order identifier used for idempotency and mapping.
- **Reconcile**: aligning local state with the Exchange (REST — source of truth).
- **Capability** (in the context of a single exchange): supported market features of the Exchange — batch, post-only, position mode, etc.

---

## 3) Scope v1 (single exchange)

### Exchange and markets

- **Exchange:** one specific exchange (e.g. Binance / OKX / Bybit).
- **Markets v1:**
  - USD-M Perpetual Futures (primary priority).
  - Spot.
- Out of v1 scope: inverse-perp, margin, options (will be added in separate iterations if needed).

### API coverage v1 (by functional area)

- **Trading (core):**
  - CreateOrder (limit/market, TIF: GTC/IOC/FOK/GTX, post-only if supported).
  - CancelOrder, ModifyOrder (if the Exchange supports modification).
  - Batch create/modify/cancel (if the Exchange supports it).
  - CancelAllOrders, CancelForgottenOrders (TTL).
- **Account/position:**
  - GetPosition / GetSymbolPosition.
  - GetOpenOrders.
  - ClosePosition (market close, if applicable).
  - GetSymbolInfo / ExchangeInfo (filters, precision, tickSize, etc.).
- **Market data:**
  - Consistent order book (snapshot + delta + seq + gap detection + resync).
  - WatchSpread / best bid-ask.
  - WatchMarkPrice, WatchLastPrice.
  - GetHistoricalCandles (e.g. 1m).
- **Rate limits:**
  - Integration with the Exchange rate-limit policy (initialized from ExchangeInfo or static config).
- **Config:**
  - SDK configuration for this Exchange (keys, base URL, WS URL, timeouts, reconnect policies).

---

## 4) Use cases (single exchange)

### Trading

- **Order creation with ClientOrderId:**
  - Create a limit/market order for a symbol with a given ClientOrderId.
  - Receive OrderInfo with OrderID, ClientOrderId, price, size, and creation time.
- **Batch order management:**
  - Send multiple create/modify/cancel in a single call (if supported by the Exchange).
  - Handle partial success (some orders accepted, some rejected).
- **Global cancel:**
  - CancelAllOrders(symbol) — guaranteed to clear all active orders for the symbol.
  - CancelForgottenOrders(symbol, TTL) — cancel "stuck" orders older than the given duration.

### Account / positions

- **Fetching a position:**
  - Current size and average entry price for a symbol.
- **Position monitoring:**
  - Subscribe to position change events (WebSocket, if supported by the Exchange).
- **Closing a position:**
  - Market close of the current position (one-shot command).

### Market data

- **Consistent order book:**
  - Fetch order book snapshot (REST).
  - Subscribe to delta stream (WS) with seq/lastUpdateId.
  - Detect gaps, perform resync.
- **Spread and prices:**
  - Subscribe to best bid/ask (spread).
  - Subscribe to mark price, last price.
- **Historical data:**
  - Fetch 1-minute candles for a given period/count.

### Rate limits and operations

- **Rate-limit-aware calls:**
  - Check/update limits before each REST/WS call.
  - On exceeded limits — controlled errors/actions (rate limit exceeded).

---

## 5) Functional requirements (per SDK module)

### 5.1 REST Client

- **Initialization:**
  - Accepts API key/secret, base URL, options (timeouts, proxy, user agent).
- **Methods:**
  - Typed services in the go-binance style (optional): NewCreateOrderService(), NewCancelOrderService(), NewDepthService(), etc.
  - Or a compact domain API: CreateOrder(ctx, CreateOrderRequest) (OrderInfo, error), CancelOrder(ctx, CancelOrderRequest) error, GetOpenOrders(ctx, symbol) ([]OrderInfo, error).
- **Requirements:**
  - Request signing per Exchange specification.
  - Response parsing into internal SDK types.
  - Error handling (HTTP level, exchange codes, network errors).

### 5.2 WebSocket Client

- **Functionality:**
  - Connection management (connect, reconnect with backoff + jitter).
  - Subscriptions to: orderbook deltas, user data (orders, balances, positions), mark/last price, best bid-ask.
- **API:**
  - Functions like: WatchOrderbook(ctx, symbol, handler, errHandler), WatchSpread(ctx, symbol, handler, errHandler), WatchPosition(ctx, symbol, handler, errHandler).
  - Cancellation via ctx.Done().

### 5.3 Orderbook engine

- **Responsibilities:**
  - Receive snapshot (REST) → initialize local order book.
  - Apply delta (WS) by seq/lastUpdateId.
  - Detect gaps (missed updates): on seq mismatch — request new snapshot + reapply delta.
  - Validation: no negative sizes, level sorting, bid/ask consistency.
- **API:**
  - Internal module/package: OrderbookEngine type with ApplySnapshot, ApplyDelta, GetTopLevels.
  - Emits aggregated updates to the outside (best bid/ask, depth up to N levels).

### 5.4 Order lifecycle

- **Responsibilities:**
  - Assign/validate ClientOrderId (if not set by the user).
  - ClientOrderId ↔ ExchangeOrderId mapping.
  - Implement CancelForgottenOrders(symbol, TTL): fetch open orders, filter by age, cancel eligible ones.
- **Note:** Do not introduce a general order status model (enum) at the SDK level in v1; if needed — only at the desk level.

### 5.5 Config module

- Configuration struct for the SDK of a single Exchange:
  - API key/secret (preferably from env).
  - Base REST URL, WS URL.
  - Request timeouts.
  - Reconnect settings (initial backoff, max backoff, jitter).
  - Orderbook settings (snapshot size, max depth, gap policy).

### 5.6 Errors and code mapping

- The SDK must have a single type/set of error types: e.g. ErrorKind (Network, RateLimit, Auth, InvalidRequest, Exchange, Unknown).
- Mapping of exchange codes (e.g. for Binance: -1021 → TimeSync, 429 → RateLimit) to these types; each error contains Kind, exchange code/message, and a wrapped error for errors.Is/As.

---

## 6) Non-functional requirements (perf/reliability/DX/security)

- **Performance:** Target — software latency (inside the SDK) of ≤ 100 µs on critical paths (parsing one WS message, applying one delta to the order book). Minimize allocations: reuse buffers, use fast JSON (json-iterator, etc.) where needed.
- **Reliability:** Reconnect with backoff + jitter; automatic resubscribe after reconnect; on critical errors — deterministic termination of Watch functions (via errHandler and error return).
- **DX:** Clear method and struct names; documentation (GoDoc) with examples for main operations; API convenient for wrapping in the unified desk interface.
- **Security:** Secrets are not logged; support for passing keys via config or env; proper sanitization of sensitive data when necessary.

---

## 7) Architecture (options and choice, or own decision)

### Option A: Service-based (go-binance style)

- **Structure:** Client with HTTP client and signing; a dedicated Service per REST endpoint with chain-style API and Do(ctx); separate functions/types for WS (WsDepthServe, WsUserDataServe, etc.).
- **Pros:** Familiar; easy to match exchange documentation; small services.
- **Cons:** Lots of code/boilerplate; tight coupling to the specific Exchange documentation.

### Option B: Domain-based

- **Structure:** Fewer, "fatter" domain interfaces: TradingClient (place/cancel/batch), AccountClient (positions, orders, leverage), MarketDataClient (orderbook, prices, candles), WsClient (subscriptions). One Client assembles them and exposes externally; internally may use smaller services.
- **Pros:** Simpler API for the user; closer to what the desk expects (Trade / Data / Account).
- **Cons:** Slightly more abstract than a direct mapping to exchange documentation.

---

## 8) Public API design (descriptive + desk compatibility)

- **Single entry point:** NewClient(config) (*Client, error) — returns the "main" SDK client for the Exchange.
- **Domain sub-clients:** client.Trading() → interface with CreateOrder, CancelOrder, BatchCreateOrders, etc.; client.Account() → GetPosition, GetOpenOrders, SetLeverage, SetPositionMode; client.MarketData() → GetOrderBook, GetHistoricalCandles; client.WS() → WatchOrderbook, WatchSpread, WatchPosition.
- **Easy desk wrapping:** External SDK types are as close as possible to desk types (CreateOrderRequest, OrderInfo, PositionInfo) or map without loss; it should be possible to implement an ExchangeConnector adapter that simply delegates to SDK methods.

---

## 9) Data models & mappings (tables)

Example for a single Exchange (accounting for its API):

- **Order types:** Exchange: LIMIT, MARKET, LIMIT_MAKER, etc. → SDK: OrderTypeLimit, OrderTypeMarket, PostOnly (if needed), time in force — GTC/IOC/FOK/GTX.
- **Time in force:** Direct mapping to SDK enum.
- **Symbol info:** From ExchangeInfo (or equivalent) to SDK SymbolInfo (min/max price, tickSize, stepSize, minNotional, precision).
- **Position:** Exchange fields (positionAmt, entryPrice, etc.) → SDK PositionInfo.

Precise mapping tables for a specific Exchange are described in a separate section/file (contract docs); the SDK must have stable domain types and clearly document field correspondence and exchange values.

---

## 10) Sequence flows (text diagrams)

### 10.1 Order creation

```
Client           TradingClient(SDK)           Exchange REST API
  | CreateOrder(ctx, req)   |                      |
  |------------------------>|  build REST request  |
  |                         |--------------------->|
  |                         |<---------------------|
  |        OrderInfo        |  parse & map         |
  |<------------------------|                      |
```

### 10.2 Orderbook (snapshot + delta + resync)

```
Client          MarketDataClient + WS         REST          WS
  | Subscribe(symbol)         |                |            |
  |-------------------------->| Get snapshot   |            |
  |                           |--------------->|            |
  |                           |<---------------| snapshot   |
  |     initial orderbook     |                |            |
  |<--------------------------|                |            |
  |                           | subscribe WS                |
  |                           |---------------------------->|
  |                           |<----------------------------| delta(seq)
  |  updated book / spread    | apply delta                 |
  |<--------------------------|                             |
  | (if gap)                  |                             |
  |                           | resync: Get snapshot        |
```

---

## 11) Error handling & retry policy

- **Error classification:** Network (timeout, connection reset, DNS), RateLimit, Auth (invalid key, signature), InvalidRequest (validation or semantic errors), Exchange (unhandled exchange codes).
- **Retry:** Network/transient — with backoff + jitter; RateLimit — based on Exchange headers/response (or fixed delays); InvalidRequest/Auth — no retry.
- **REST vs WS:** REST errors are returned to the caller as error with Kind and details; WS errors go to errHandler; on critical error, the Watch method terminates.

---

## 12) Rate limit policy

- Implementing a **cross-exchange** rate limiter is not a required SDK component, but the SDK must be able to read and where possible return rate-limit headers/metadata (exchange usage counters) and map 429 and special codes to RateLimit errors.
- Optional: built-in simple token bucket, enabled via config parameter, with method categories (Order/Cancel/Query/MarketData).

---

## 13) Testing strategy

- **Unit tests:** Parsing JSON responses into SDK structs; mapping exchange errors to SDK types; orderbook logic (applying snapshot + delta, gap detection, resync).
- **Contract tests:** JSON fixtures from real Exchange responses; "contract change" tests — if the format changes, tests fail.
- **Integration tests (optional):** Against testnet (if available) or a mock server: place → GetOrder → cancel; subscribe to WS and verify event format/frequency.

---

## 14) Milestones & deliverables

Example for a single Exchange:

1. **M1 — REST core:** Basic REST client (signing, timeouts); methods CreateOrder, CreateBatchOrder, ModifyOrder, ModifyBatchOrder, CancelOrder, GetOpenOrders, GetSymbolInfo.
2. **M2 — Orderbook & Market data:** GetOrderBook (snapshot); WS subscriptions for depth/bid&ask spread/prices; orderbook engine with gap detection and resync.
3. **M3 — Account & position:** GetPosition, WatchPosition, ClosePosition; CancelAllOrders, CancelForgottenOrders.
4. **M4 — Rate limits & errors:** Mapping of main exchange codes to SDK error types; optional built-in rate limit helper.
5. **M5 — Documentation & examples:** GoDoc, README, usage examples (simple market maker / tester).

---

## 15) Acceptance criteria (Definition of Done)

- SDK covers all APIs in Scope v1 for the given Exchange.
- Orderbook engine ensures order book consistency (snapshot + delta + seq + resync).
- All main operations use ClientOrderId or correctly map exchange identifiers.
- Errors are classified; exchange code mapping verified by contract tests.
- There is a code example integrating the SDK with the unified desk interface (ExchangeConnector).
- No goroutine leaks on context cancellation and WS subscription close.

---

*End of specification.*
