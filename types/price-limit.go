/*
ФАЙЛ: types/price-limit.go

ОПИСАНИЕ:
PriceLimit — текущие верхняя и нижняя границы допустимых цен ордера на
инструменте. OKX отклоняет ордера за пределами этих границ с code 51004.

Маппится из:
  - GET /api/v5/public/price-limit?instId=...
  - WS public channel "price-limit"

HFT-применение:
  - pre-trade validation: проверить, что цена ордера попадает в [SellLmt,
    BuyLmt] до отправки, чтобы не получить 51004 и не тратить лимиты;
  - детектор фазы рынка: резкое сужение границ часто предшествует halt
    или волатильному движению.
*/

package types

import "github.com/shopspring/decimal"

// PriceLimit — границы допустимых цен ордера.
type PriceLimit struct {
	// InstType — тип инструмента.
	InstType InstType
	// InstID — идентификатор инструмента.
	InstID string
	// BuyLmt — максимальная цена buy-ордера (buyLmt).
	BuyLmt decimal.Decimal
	// SellLmt — минимальная цена sell-ордера (sellLmt).
	SellLmt decimal.Decimal
	// Ts — таймштамп snapshot'а в мс (ts).
	Ts int64
}
