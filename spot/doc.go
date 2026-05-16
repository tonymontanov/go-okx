/*
Package spot — SPOT-профиль SDK OKX v5.

Назначение: торговля и стримы рыночных данных по спот-инструментам OKX
(instType=SPOT, instID вида "BTC-USDT"). Архитектурно зеркально пакету
github.com/tonymontanov/go-okx/v2/swap — те же саб-клиенты, тот же транспорт,
тот же стиль колбэков для WS.

# Подключение

Чтобы использовать spot через корневой okx.Client, достаточно blank-import:

	import (
	    okx "github.com/tonymontanov/go-okx/v2"
	    "github.com/tonymontanov/go-okx/v2/spot"
	    _ "github.com/tonymontanov/go-okx/v2/spot" // регистрация фабрики в окх
	)

	client, _ := okx.NewClient(cfg)
	spotClient := client.Spot().(*spot.Client)

# Что отличается от swap

  - Нет понятия "контракт": все размеры (sz) в БАЗОВОЙ валюте. Конверсия
    base↔contracts здесь не нужна.
  - Нет hedge-режима (PosSide, ReduceOnly): на cash-споте можно только
    купить и продать, нельзя шортить.
  - SymbolInfo не содержит CtVal/CtMult.
  - tdMode по умолчанию = "cash" (для cash trading); для spot margin —
    выставляйте TdMode явно (TdModeCross/TdModeIsolated).
  - На market BUY OKX по умолчанию считает sz в КОТИРОВОЧНОЙ валюте
    (USDT для BTC-USDT). Чтобы купить точно N единиц базовой валюты,
    задавайте CreateOrderRequest.TgtCcy = TgtCcyBase.

# Что общее с swap

  - Транспорт REST/WS (internal/rest, internal/ws) — переиспользуется как есть.
  - Unified-account: один баланс /api/v5/account/balance на spot + swap.
    Тип Balance переиспользуется через алиас (spot/types/aliases.go).
  - Enum'ы протокола: SideType, OrderType, TimeInForceType, InstType,
    OrderState, OrderBookLevel, AggTrade, Candle, Timeframe, QuotedSpreadUpdate —
    тоже реэкспортированы как алиасы на swap/types, чтобы код был
    совместим без приведений.

# Margin trading на споте

В текущей версии SDK поддержан только cash-режим. Поля TdMode, Ccy в
CreateOrderRequest объявлены и проходят в REST как есть, но отдельных
helper-методов для маржинального шорта/займа НЕТ. Добавим в следующих
версиях по реальной потребности.
*/
package spot
