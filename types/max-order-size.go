/*
ФАЙЛ: types/max-order-size.go

ОПИСАНИЕ:
MaxOrderSize — максимально допустимый размер ордера для инструмента в
указанном режиме маржи. Учитывает текущий баланс, плечо и существующие
открытые позиции/ордера.

Маппится из:
  - GET /api/v5/account/max-size      — макс размер БЕЗ учёта pending ордеров
  - GET /api/v5/account/max-avail-size — макс размер С учётом pending ордеров

HFT-применение:
  - pre-trade sizing: запрашиваем перед серией ордеров, чтобы не уйти в
    51008 (insufficient balance) или 51020 (max position size exceeded);
  - rebalance: считаем разницу между «макс long» и «макс short», чтобы
    понять текущее использование маржи.
*/

package types

import "github.com/shopspring/decimal"

// MaxOrderSize — максимальный размер ордера.
type MaxOrderSize struct {
	// InstID — инструмент.
	InstID string
	// Ccy — валюта (для MARGIN/SPOT) (ccy).
	Ccy string
	// MaxBuy — максимальный размер buy-ордера в базовой валюте/контрактах
	// (maxBuy).
	MaxBuy decimal.Decimal
	// MaxSell — максимальный размер sell-ордера (maxSell).
	MaxSell decimal.Decimal
}

// MaxOrderSizes — слайс.
type MaxOrderSizes []MaxOrderSize
