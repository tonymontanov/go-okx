/*
ФАЙЛ: swap/types/create-order-request.go

ОПИСАНИЕ:
Структура запроса на создание ордера в SWAP-секции OKX. Сделана максимально
близкой к `connectors/types.CreateOrderRequest` деска, чтобы адаптер
OKXFuturesConnector мог обходиться без потерь информации, но при этом
расширена OKX-специфичными полями (TdMode, PosSide, Ccy).

ПОЛЯ:
  - InstID         — инструмент в формате OKX (например, "BTC-USDT-SWAP").
  - Side           — buy/sell.
  - OrderType      — limit/market/post_only/fok/ioc. Если задан явный OrderType,
                     поле TimeInForce ИГНОРИРУЕТСЯ. Это сделано чтобы адаптер
                     из core (где есть только TIF) и нативные пользователи SDK
                     (где есть OrderType) одинаково корректно работали.
  - TimeInForce    — TIF в нотации core/Binance (GTC/IOC/FOK/GTX). Конвертируется
                     в OrderType, если OrderType пуст.
  - Size           — количество в контрактах (lotSize). decimal.Decimal для
                     точности; при сборке запроса конвертируется в строку без
                     потерь.
  - Price          — цена (для limit/post_only/fok/ioc). Для market не нужна.
  - ClientOrderID  — клиентский id (1..32 символа [A-Za-z0-9_]).
  - ReduceOnly     — флаг ReduceOnly. Включается также автоматически в OptimalLimitIOC.
  - TdMode         — margin-mode. Если пустой — выводится автоматически (cross
                     для обычного ордера; см. ResolveTdMode в swap/client).
  - PosSide        — сторона позиции. Пустой → "net" в net-mode (default).
  - Ccy            — валюта маржи для cross/isolated. Пустая → выводится из инструмента.
  - Tag            — broker tag (опционально, для OKX broker program).

ИНВАРИАНТЫ:
  - При OrderType=="market" поле Price игнорируется при сборке запроса.
  - При OrderType=="post_only" сервер отклонит ордер, который немедленно исполнится
    как taker; SDK эту проверку не дублирует.
*/

package types

import "github.com/shopspring/decimal"

// CreateOrderRequest — запрос на создание ордера SWAP.
type CreateOrderRequest struct {
	InstID        string
	Side          SideType
	OrderType     OrderType
	TimeInForce   TimeInForceType
	Size          decimal.Decimal
	Price         decimal.Decimal
	ClientOrderID string
	ReduceOnly    bool
	TdMode        TdMode
	PosSide       PosSide
	Ccy           string
	Tag           string
}
