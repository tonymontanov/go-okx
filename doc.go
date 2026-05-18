/*
ФАЙЛ: doc.go

ОПИСАНИЕ:
Пакет okx — это корневой пакет SDK для биржи OKX (REST v5 + WebSocket v5),
рассчитанный на высокочастотный трейдинг. Здесь живут общие для всех профилей
сущности: Config, Logger, ErrorKind/Error, rate-limit события, а также
«главный» Client, который раздаёт под-клиенты по продуктовым секциям.

ОСНОВНАЯ ИДЕЯ:
SDK построена по domain-based архитектуре: пользователю выдаются доменные
саб-клиенты (Trading/Account/MarketData/Stream), а низкоуровневые сервисы
(подпись запросов, HTTP-pool, WS-pool, парсинг ответов) скрыты в internal-
пакетах. Это позволяет одновременно:
  - давать минималистичный публичный API;
  - не тянуть в сторонние проекты половину внутренностей через `go doc`.

ПРОДУКТОВЫЕ СЕКЦИИ:
  v2.x (released):
    - swap   (USD-M Perpetual, instType=SWAP)
    - spot   (instType=SPOT)

  v2.5-dev (in progress):
    - foundation: WS request/reply framing, расширенные общие types;
    - critical HFT: WS Order API, cancel-all-after, расширенные WS books-
      каналы (books5/books-l2-tbt/books50-l2-tbt), fills (REST+WS);
    - perp-analytics: ticker/mark-price/funding/open-interest/liquidations
      (REST+WS) + system time/status + orders-history;
    - account/risk: account/config, max-size, leverage-info, risk-state,
      bills, set-account-level/greeks;
    - capital management: asset/ (funding account), subaccount/;
    - доп. секции: margin (instType=MARGIN), futures (instType=FUTURES),
      options (instType=OPTION) — каждая как top-level пакет.

ОБЩИЙ ПРОТОКОЛЬНЫЙ СЛОЙ:
Все общие enum-ы (SideType, OrderType, TimeInForce, TdMode, InstType,
OrderState) и нейтральные структуры (OrderBook*, Candle, AggTrade, Balance,
Ticker, FundingRate, MarkPrice и т.д.) живут в пакете
`github.com/tonymontanov/go-okx/v2/types`. Профильные пакеты (swap/types,
spot/types и т.д.) при необходимости алиасят оттуда. Это устраняет любой
кросс-импорт между продуктовыми секциями и позволяет писать кросс-секционный
код без import-циклов.

ЗАВИСИМОСТИ:
- github.com/json-iterator/go     — быстрый JSON для hot-path;
- github.com/gorilla/websocket    — WS-соединения;
- github.com/shopspring/decimal   — точные числа для цен/количеств.
*/
package okx
