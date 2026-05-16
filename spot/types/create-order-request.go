/*
ФАЙЛ: spot/types/create-order-request.go

ОПИСАНИЕ:
Запрос на создание ордера SPOT. Отличия от SWAP-варианта:
  - НЕТ PosSide (на споте нет hedge-режима).
  - НЕТ ReduceOnly (на cash-споте нельзя шортить — нечего "уменьшать").
  - TdMode по умолчанию `cash` (а не `cross`).
  - Size — в БАЗОВОЙ валюте (например 0.1 для 0.1 BTC), а не в контрактах.
  - Для market BUY OKX по умолчанию интерпретирует `sz` как количество
    КОТИРОВОЧНОЙ валюты (USDT для BTC-USDT). Если хотите купить точно N
    единиц базовой валюты — выставляйте TgtCcy = TgtCcyBase.

ИНВАРИАНТЫ:
  - Для OrderType=Market на BUY: либо передавайте Size в quote-валюте и не
    задавайте TgtCcy (поведение OKX по умолчанию), либо передавайте
    TgtCcy=TgtCcyBase и Size в базовой валюте.
  - Для всех остальных OrderType (limit/post_only/fok/ioc): Size в БАЗОВОЙ
    валюте, Price обязательна.
*/

package types

import "github.com/shopspring/decimal"

// TgtCcy — какую валюту OKX должен считать единицей измерения Size для
// market-ордеров на спот. По умолчанию для buy = quote_ccy (USDT), для
// sell = base_ccy (BTC). Для limit-ордеров параметр ИГНОРИРУЕТСЯ — там
// Size всегда в базовой валюте.
type TgtCcy string

const (
	// TgtCcyBase — Size трактуется как количество базовой валюты (BTC).
	TgtCcyBase TgtCcy = "base_ccy"
	// TgtCcyQuote — Size трактуется как сумма в котировочной валюте (USDT).
	TgtCcyQuote TgtCcy = "quote_ccy"
)

// CreateOrderRequest — запрос на создание ордера SPOT.
type CreateOrderRequest struct {
	// InstID — инструмент в формате OKX SPOT (например, "BTC-USDT").
	InstID string

	// Side — buy/sell.
	Side SideType

	// OrderType — limit/market/post_only/fok/ioc. OptimalLimitIOC на SPOT
	// не поддерживается биржей; SDK не валидирует, OKX вернёт ошибку.
	// Если задан явный OrderType — поле TimeInForce ИГНОРИРУЕТСЯ.
	OrderType OrderType

	// TimeInForce — TIF в нотации Binance-style (GTC/IOC/FOK/GTX). Конвертируется
	// в OrderType, если OrderType пуст.
	TimeInForce TimeInForceType

	// Size — количество в БАЗОВОЙ валюте (например, 0.1 BTC). Для market BUY
	// может быть трактовано как quote-валюта в зависимости от TgtCcy.
	Size decimal.Decimal

	// Price — цена для limit-ордеров. Игнорируется для market.
	Price decimal.Decimal

	// ClientOrderID — клиентский id (1..32 символа [A-Za-z0-9], только буквы
	// и цифры; биржа отклонит подчёркивания/дефисы/точки кодом 51000).
	ClientOrderID string

	// TdMode — margin-mode. Если пустой — выводится в TdModeCash (default для SPOT).
	// Для spot margin trading (cross/isolated) пользователь должен задать явно.
	TdMode TdMode

	// TgtCcy — единица измерения Size для market-ордеров. См. doc-комментарий
	// типа TgtCcy. Для limit-ордеров параметр игнорируется (sz всегда base).
	// Если пустой — используется OKX-default (buy=quote, sell=base).
	TgtCcy TgtCcy

	// Ccy — валюта маржи (для spot margin trading). На cash-споте не нужна.
	Ccy string

	// Tag — broker tag (опционально, для OKX broker program).
	Tag string
}
