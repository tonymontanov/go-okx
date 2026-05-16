/*
Package types — протокольно-общие типы OKX v5, разделяемые всеми профилями
(spot, swap, в будущем futures/options).

ЗАЧЕМ ОТДЕЛЬНЫЙ ПАКЕТ:
Раньше "общие" enum'ы и data-структуры (SideType, OrderType, OrderBookLevel,
Balance, ...) жили в swap/types, а spot/types ссылался на них алиасами. Это
создавало логически некорректную зависимость spot → swap: spot-профиль не
должен знать о существовании swap, оба должны быть равноправными
консьюмерами протокольного слоя.

Этот пакет — выделенный нейтральный слой:

	github.com/tonymontanov/go-okx/v2/types/  ← общий протокольный слой (этот пакет)
	    ↑                          ↑
	    │                          │
	swap/types/                spot/types/   ← профильные пакеты, алиасы + свои типы

ЧТО ЛЕЖИТ ЗДЕСЬ:
  - Enum'ы протокола OKX, общие для всех instType: SideType, OrderType (общие
    значения), TimeInForceType, TdMode, InstType, OrderState + ParseOrderState.
  - Структуры WS/REST одинакового формата для всех профилей: OrderBookLevel,
    OrderBookSnapshot, Candle/Candles, Timeframe, AggTrade, QuotedSpreadUpdate.
  - Unified-account модель: Balance, BalanceDetail (OKX отдаёт ОДИН баланс на
    пользователя для всех профилей).

ЧТО НЕ ЛЕЖИТ ЗДЕСЬ:
  - Профильные типы запросов: CreateOrderRequest, ModifyOrderRequest,
    CancelOrderRequest, OrderInfo, SymbolInfo. У spot и swap они отличаются
    набором полей (PosSide/ReduceOnly есть только у swap; TgtCcy — только
    у spot; SymbolInfo.CtVal — только у swap и т. д.). См. swap/types/* и
    spot/types/*.
  - Enum'ы и константы, специфичные для одного профиля: PosSide, PositionMode
    (swap-only, hedge mode); OrderTypeOptimalLimitIOC (swap-only ord type).
    Они живут в swap/types и работают поверх общего OrderType.

BACKWARDS COMPATIBILITY:
Существующие пользователи продолжают импортировать swap/types и spot/types и
обращаться к типам через них — оба пакета type-alias'ятся на этот. Прямой
импорт github.com/tonymontanov/go-okx/v2/types не обязателен.

КОДСТАЙЛ:
Этот пакет следует тем же правилам, что и остальной SDK:
  - имена файлов в kebab-case;
  - один тип на файл, где смысл этого оправдан;
  - struct-fields с decimal.Decimal без эпсилон-сравнений;
  - проектная аннотация в шапке файла.
*/
package types
