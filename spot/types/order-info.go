/*
ФАЙЛ: spot/types/order-info.go

ОПИСАНИЕ:
Информация об ордере SPOT. Структурно совпадает с SWAP OrderInfo
(см. swaptypes.OrderInfo) — единственное отличие: на SPOT Size в БАЗОВОЙ
валюте, а не в контрактах. Поэтому делаем отдельный type, чтобы документация
поля Size была корректной.
*/

package types

import "github.com/shopspring/decimal"

// OrderInfo — информация об ордере SPOT.
type OrderInfo struct {
	// OrderID — биржевой id (ordId).
	OrderID string
	// ClientOrderID — клиентский id (clOrdId).
	ClientOrderID string
	// InstID — инструмент в формате OKX SPOT (например, "BTC-USDT").
	InstID string
	// Side — направление.
	Side SideType
	// OrderType — тип ордера.
	OrderType OrderType
	// Price — цена ордера.
	Price decimal.Decimal
	// Size — размер в БАЗОВОЙ валюте (например, 0.1 BTC).
	Size decimal.Decimal
	// FilledSize — сколько исполнено (accFillSz), в БАЗОВОЙ валюте.
	FilledSize decimal.Decimal
	// State — статус ордера.
	State OrderState
	// CreatedAtMs — таймштамп создания (миллисекунды).
	CreatedAtMs int64
	// UpdatedAtMs — таймштамп последнего обновления.
	UpdatedAtMs int64
	// RateLimits — слепок заголовков rate-limit, полученных вместе с ответом.
	RateLimits map[string]string
}
