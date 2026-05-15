# Техническое задание: SDK под одну биржу (single-exchange)

**Версия:** 1.0  
**Дата:** 2025-02  
**Контекст:** Отдельная SDK на биржу в Go; унификация — в коде деска. Референс стиля API: adshao/go-binance v2.8.9.

---

## 1) Overview и цели

**Цель:**  
Разработать высокопроизводительную Go-SDK для **одной конкретной биржи** (далее — *Биржа*), ориентированную на HFT/алготрейдинг, которая:

- покрывает REST + WebSocket API данной Биржи;
- даёт удобный, идиоматичный Go-API (в духе adshao/go-binance);
- обеспечивает корректность orderbook и состояния ордеров/позиций;
- легко оборачивается в существующий унифицированный интерфейс деска (`ExchangeConnector`), но сама по себе **не является мульти-биржевой абстракцией**.

---

## 2) Glossary (термины)

- **SDK**: Go-библиотека для работы с одной Биржей (REST + WS).
- **REST Client**: компонент SDK, реализующий HTTP-вызовы к REST API Биржи.
- **WS Client**: компонент SDK, реализующий подключения и подписки к WebSocket API.
- **Orderbook engine**: часть SDK, которая обеспечивает консистентный стакан (snapshot + delta + seq + gap detection + resync).
- **Order lifecycle**: полное жизненное состояние ордера — от создания до финального статуса.
- **ClientOrderId**: клиентский идентификатор ордера, используемый для идемпотентности и маппинга.
- **Reconcile**: согласование локального состояния с Биржей (REST — source of truth).
- **Capability** (в контексте одной биржи): поддерживаемые особенности рынков данной Биржи — batch, post-only, position mode и т.п.

---

## 3) Scope v1 (для одной биржи)

### Биржа и рынки

- **Биржа:** одна конкретная (например, Binance / OKX / Bybit).
- **Рынки v1:**
  - USD-M Perpetual Futures (основной приоритет).
  - Spot.
- Вне v1: inverse-perp, margin, options (при необходимости будут добавлены отдельными итерациями).

### Охват API v1 (по функциональным областям)

- **Trading (core):**
  - CreateOrder (limit/market, TIF: GTC/IOC/FOK,GTX, post-only если есть).
  - CancelOrder, ModifyOrder (если Биржа поддерживает модификацию).
  - Batch create/modify/cancel (если Биржа поддерживает).
  - CancelAllOrders, CancelForgottenOrders (TTL).
- **Account/position:**
  - GetPosition / GetSymbolPosition.
  - GetOpenOrders.
  - ClosePosition (market close, если применимо).
  - GetSymbolInfo / ExchangeInfo (фильтры, precision, tickSize и т.п.).
- **Market data:**
  - Консистентный orderbook (snapshot + delta + seq + gap detection + resync).
  - WatchSpread / best bid-ask.
  - WatchMarkPrice, WatchLastPrice.
  - GetHistoricalCandles (например, 1m).
- **Rate limits:**
  - Интеграция с rate-limit политикой Биржи (инициализация по ExchangeInfo или статике).
- **Config:**
  - Конфигурация SDK для этой Биржи (ключи, base URL, WS URL, таймауты, политики reconnect).

---

## 4) Use cases (для одной биржи)

### Trading

- **Создание ордера с ClientOrderId:**
  - Создать лимит/маркет ордер по символу с заданным ClientOrderId.
  - Получить OrderInfo с OrderID, ClientOrderId, ценой, объёмом и временем создания.
- **Пакетное управление ордерами:**
  - Отправить несколько create/modify/cancel в одном вызове (если поддерживается Биржей).
  - Обработать частичный успех (часть ордеров принята, часть отклонена).
- **Глобальная отмена:**
  - CancelAllOrders(symbol) — гарантировано очищает все активные ордера по символу.
  - CancelForgottenOrders(symbol, TTL) — отмена «зависших» ордеров старше заданного времени.

### Account / позиции

- **Получение позиции:**
  - Текущий размер и средняя цена входа по символу.
- **Мониторинг позиции:**
  - Подписка на события изменения позиции (WebSocket, если Биржа поддерживает).
- **Закрытие позиции:**
  - Market close текущей позиции (one-shot команда).

### Market data

- **Консистентный стакан:**
  - Получить snapshot стакана (REST).
  - Подписаться на поток delta (WS) с seq/lastUpdateId.
  - Детектировать пропуски (gap), выполнять resync.
- **Спред и цены:**
  - Подписка на best bid/ask (спред).
  - Подписка на mark price, last price.
- **Исторические данные:**
  - Получение мощёных 1-минутных свечей за заданный период/количество.

### Rate limits и операции

- **Rate-limit aware вызовы:**
  - Перед каждым REST-/WS-вызовом проверка/обновление лимитов.
  - При превышении — контролируемые ошибки/действия (rate limit exceeded).

---

## 5) Functional requirements (по модулям SDK)

### 5.1 REST Client

- **Инициализация:**
  - Принимает API-ключ/секрет, base URL, опции (таймауты, proxy, user agent).
- **Методы:**
  - Typed-сервисы в стиле go-binance (опционально): NewCreateOrderService(), NewCancelOrderService(), NewDepthService() и т.д.
  - Либо компактный доменный API: CreateOrder(ctx, CreateOrderRequest) (OrderInfo, error), CancelOrder(ctx, CancelOrderRequest) error, GetOpenOrders(ctx, symbol) ([]OrderInfo, error).
- **Требования:**
  - Подпись запросов согласно спецификации Биржи.
  - Парсинг ответов во внутренние типы SDK.
  - Обработка ошибок (HTTP-уровень, биржевые коды, сетевые ошибки).

### 5.2 WebSocket Client

- **Функциональность:**
  - Управление подключением (connect, reconnect с backoff + jitter).
  - Подписки на: orderbook deltas, user data (ордера, балансы, позиции), mark/last price, best bid-ask.
- **API:**
  - Функции вида: WatchOrderbook(ctx, symbol, handler, errHandler), WatchSpread(ctx, symbol, handler, errHandler), WatchPosition(ctx, symbol, handler, errHandler).
  - Отмена через ctx.Done().

### 5.3 Orderbook engine

- **Обязанности:**
  - Получение snapshot (REST) → инициализация локального стакана.
  - Применение delta (WS) по seq/lastUpdateId.
  - Обнаружение gap (пропущенных обновлений): при несовпадении seq — запрос нового snapshot + повторное применение delta.
  - Валидация: отсутствие отрицательных размеров, сортировка уровней, согласованность bid/ask.
- **API:**
  - Внутренний модуль/пакет: тип OrderbookEngine с ApplySnapshot, ApplyDelta, GetTopLevels.
  - Выдаёт агрегированные updates наружу (best bid/ask, глубина до N уровней).

### 5.4 Order lifecycle

- **Обязанности:**
  - Присвоение/валидирование ClientOrderId (если не задан пользователем).
  - Маппинг ClientOrderId ↔ ExchangeOrderId.
  - Реализация CancelForgottenOrders(symbol, TTL): получение открытых ордеров, фильтрация по возрасту, отмена подходящих.
- **Важно:** В v1 не вводить общую модель статусов ордера (enum) на уровень SDK; по необходимости — только на уровне деска.

### 5.5 Config module

- Конфигурационная структура для SDK одной Биржи:
  - API-ключ/секрет (по возможности из env).
  - Base REST URL, WS URL.
  - Таймауты запросов.
  - Настройки reconnect (initial backoff, max backoff, jitter).
  - Настройки orderbook (размер snapshot, max depth, policy при gap).

### 5.6 Ошибки и маппинг кодов

- В SDK должен быть единый тип/набор типов ошибок: например ErrorKind (Network, RateLimit, Auth, InvalidRequest, Exchange, Unknown).
- Маппинг биржевых кодов (например, для Binance: -1021 → TimeSync, 429 → RateLimit) в эти типы; каждая ошибка содержит Kind, биржевой код/сообщение и обёрнутый error для errors.Is/As.

---

## 6) Non-functional requirements (perf/reliability/DX/security)

- **Производительность:** Цель — программная задержка (внутри SDK) порядка ≤ 100 мкс на критических путях (парсинг одного WS-сообщения, применение одной delta к стакану). Минимизация аллокаций: повторное использование буферов, при необходимости быстрый JSON (json-iterator и т.п.).
- **Надёжность:** Reconnect с backoff + jitter; автоматическое resubscribe после reconnect; при критических ошибках — детерминированное завершение Watch-функций (через errHandler и возврат ошибки).
- **DX:** Понятные имена методов и структур; документация (GoDoc) с примерами для основных операций; API, удобный для обёртывания в унифицированный интерфейс деска.
- **Security:** Секреты не логируются; поддержка передачи ключей через конфиг или env; корректная очистка чувствительных данных при необходимости.

---

## 7) Architecture (варианты и выбор, либо на собственное решение)

### Вариант A: Service-based (в стиле go-binance)

- **Структура:** Client с HTTP-клиентом и подписью; для каждого REST-эндпоинта — свой Service с chain-style API и Do(ctx); отдельные функции/типы для WS (WsDepthServe, WsUserDataServe и т.п.).
- **Плюсы:** Привычно; легко соответствовать документации Биржи; мелкие сервисы.
- **Минусы:** Много кода/boilerplate; сильная связка с документацией конкретной Биржи.

### Вариант B: Domain-based

- **Структура:** Меньшее количество более «толстых» доменных интерфейсов: TradingClient (place/cancel/batch), AccountClient (positions, orders, leverage), MarketDataClient (orderbook, prices, candles), WsClient (подписки). Один Client собирает их и предоставляет наружу; внутри могут использоваться более мелкие сервисы.
- **Плюсы:** API проще для пользователя; ближе к тому, что ждёт деск (Trade / Data / Account).
- **Минусы:** Чуть более абстрактно, чем «в лоб» по документации.

---

## 8) Public API design (описательно + совместимость с деском)

- **Единая точка входа:** NewClient(config) (*Client, error) — возвращает «главный» клиент SDK для данной Биржи.
- **Доменные под-клиенты:** client.Trading() → интерфейс с CreateOrder, CancelOrder, BatchCreateOrders и т.д.; client.Account() → GetPosition, GetOpenOrders, SetLeverage, SetPositionMode; client.MarketData() → GetOrderBook, GetHistoricalCandles; client.WS() → WatchOrderbook, WatchSpread, WatchPosition.
- **Лёгкость обёртки в деск:** Внешние типы SDK по возможности близки к типам деска (CreateOrderRequest, OrderInfo, PositionInfo) или маппятся без потерь; можно реализовать адаптер ExchangeConnector, который просто делегирует в методы SDK.

---

## 9) Data models & mappings (таблицы)

Пример для одной Биржи (с учётом её API):

- **Order types:** Биржа: LIMIT, MARKET, LIMIT_MAKER и т.д. → SDK: OrderTypeLimit, OrderTypeMarket, PostOnly (если нужно), время в силе — GTC/IOC/FOK/GTX.
- **Time in force:** Прямой маппинг в enum SDK.
- **Symbol info:** Из ExchangeInfo (или аналога) в SymbolInfo SDK (min/max price, tickSize, stepSize, minNotional, precision).
- **Position:** Биржевые поля (positionAmt, entryPrice и т.п.) → PositionInfo SDK.

Точные таблицы маппинга для конкретной Биржи описываются в отдельном разделе/файле (contract docs); SDK обязана иметь стабильные доменные типы и чётко документировать соответствие полей и биржевых значений.

---

## 10) Sequence flows (текстовые диаграммы)

### 10.1 Order creation

```
Client           TradingClient(SDK)           REST API Биржи
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

- **Классификация ошибок:** Network (timeout, connection reset, DNS), RateLimit, Auth (invalid key, signature), InvalidRequest (валидация или семантические ошибки), Exchange (непредусмотренные коды Биржи).
- **Retry:** Network/временные — с backoff + jitter; RateLimit — на основе заголовков/ответа Биржи (или фиксированные задержки); InvalidRequest/Auth — без retry.
- **REST vs WS:** REST-ошибки возвращаются вызывающему коду как error с Kind и деталями; WS-ошибки попадают в errHandler; при критической ошибке Watch-метод завершает работу.

---

## 12) Rate limit policy

- Реализация **перебиржевого** rate-limiter'а не входит в SDK как обязательный компонент, но SDK должна уметь читать и по возможности возвращать заголовки/метаданные по rate limit (usage counters Биржи) и маппить 429 и специальные коды в RateLimit ошибки.
- Опционально: встроенный простой токен-бакет, включаемый параметром конфигурации, с категориями методов (Order/Cancel/Query/MarketData).

---

## 13) Testing strategy

- **Unit tests:** Парсинг JSON ответов в структуры SDK; маппинг ошибок Биржи в типы SDK; логика orderbook (применение snapshot + delta, обнаружение gap, resync).
- **Contract tests:** Фикстуры JSON с реальных ответов Биржи; тесты «изменения контракта» — если формат меняется, тесты падают.
- **Integration tests (опционально):** Против testnet (если есть) или мок-сервера: place → GetOrder → cancel; подписка на WS и проверка формата/частоты событий.

---

## 14) Milestones & deliverables

Пример для одной Биржи:

1. **M1 — REST core:** Базовый REST-клиент (подпись, таймауты); методы CreateOrder, CreateBatchOrder, ModifyOrder, ModifyBatchOrder, CancelOrder, GetOpenOrders, GetSymbolInfo.
2. **M2 — Orderbook & Market data:** GetOrderBook (snapshot); WS-подписки на depth/bid&ask спред/цены; orderbook engine с gap detection и resync.
3. **M3 — Account & position:** GetPosition, WatchPosition, ClosePosition; CancelAllOrders, CancelForgottenOrders.
4. **M4 — Rate limits & ошибки:** Маппинг основных биржевых кодов в типы ошибок SDK; опциональный встроенный rate limit helper.
5. **M5 — Документация и примеры:** GoDoc, README, примеры использования (simple market maker / tester).

---

## 15) Acceptance criteria (Definition of Done)

- SDK покрывает все API в Scope v1 для данной Биржи.
- Orderbook engine обеспечивает консистентность стакана (snapshot + delta + seq + resync).
- Все основные операции используют ClientOrderId либо корректно маппят биржевые идентификаторы.
- Ошибки классифицированы, маппинг биржевых кодов проверен контракт-тестами.
- Есть пример кода, интегрирующий SDK с унифицированным интерфейсом деска (ExchangeConnector).
- Нет утечек горутин при отмене контекстов и закрытии WS-подписок.

---

*Конец ТЗ.*
