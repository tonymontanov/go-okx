/*
ФАЙЛ: doc.go

ОПИСАНИЕ:
Пакет okx — это корневой пакет SDK для биржи OKX (REST v5 + WebSocket v5),
рассчитанный на высокочастотный трейдинг. Здесь живут общие для всех профилей
(SWAP/SPOT) сущности: Config, Logger, ErrorKind/Error, Option-конфигурация,
а также «главный» Client, который раздаёт под-клиенты по профилям.

ОСНОВНАЯ ИДЕЯ:
SDK построена по domain-based архитектуре (см. ТЗ §7, Variant B): пользователю
выдаются доменные саб-клиенты (Trading/Account/MarketData/Stream), а
низкоуровневые сервисы (подпись запросов, HTTP-pool, WS-пул, парсинг ответов)
скрыты в internal-пакетах. Это позволяет одновременно:
  - давать минималистичный publik API (как в TZ §8);
  - не тянуть в сторонние проекты половину внутренностей через `go doc`.

ПРОФИЛИ ВЕРСИИ v1:
  - swap   (USD-M Perpetual, instType=SWAP) — основной приоритет.
  - spot   (instType=SPOT)                  — заглушка под следующую итерацию.

КОНТРАКТ:
Все доменные типы (CreateOrderRequest, OrderInfo, PositionInfo, SymbolInfo и т.п.)
живут в подпакетах `swap/types` и `spot/types`; общие enum-ы (SideType,
OrderType, TimeInForceType) дублируются в каждом профиле, чтобы избежать
кросс-импорта и сделать профили самодостаточными.

ЗАВИСИМОСТИ:
- github.com/json-iterator/go     — быстрый JSON для hot-path;
- github.com/gorilla/websocket    — WS-соединения;
- github.com/shopspring/decimal   — точные числа для цен/количеств.
*/
package okx
