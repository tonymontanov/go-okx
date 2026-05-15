/*
ФАЙЛ: swap/types/order-info.go

ОПИСАНИЕ:
Структура с информацией об ордере SWAP. Используется как:
  - возврат для CreateOrder/ModifyOrder/CancelOrder/Batch*;
  - элемент списка GetOpenOrders;
  - payload события WatchOpenOrders.

ПОЛЯ:
  - OrderID       — биржевой id (ordId).
  - ClientOrderID — клиентский id (clOrdId).
  - InstID        — инструмент.
  - Side          — направление.
  - Price         — цена ордера.
  - Size          — полный размер (sz, в контрактах).
  - FilledSize    — сколько исполнено (accFillSz).
  - State         — статус (live/partially_filled/filled/canceled).
  - CreatedAtMs   — таймштамп создания (cTime, миллисекунды).
  - UpdatedAtMs   — таймштамп последнего обновления (uTime).
  - RateLimits    — слепок заголовков rate-limit, полученных вместе с ответом.
    map[header]value (см. ответы на ТЗ, пункт 7).

Все decimal-значения хранятся в decimal.Decimal — точно. Адаптер в core
конвертирует их в float64 (`.InexactFloat64()`) на своей стороне.
*/

package types

import "github.com/shopspring/decimal"

// OrderInfo — информация об ордере SWAP.
type OrderInfo struct {
	OrderID       string
	ClientOrderID string
	InstID        string
	Side          SideType
	OrderType     OrderType
	Price         decimal.Decimal
	Size          decimal.Decimal
	FilledSize    decimal.Decimal
	State         OrderState
	CreatedAtMs   int64
	UpdatedAtMs   int64
	RateLimits    map[string]string
}
