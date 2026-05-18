/*
ФАЙЛ: types/mark-price.go

ОПИСАНИЕ:
MarkPrice — справедливая (mark) цена инструмента, по которой OKX считает
PnL для perp/futures/options и проводит ликвидации. Отличается от
last-trade цены: сглажена через комбинацию индекса и спреда.

Маппится из:
  - GET /api/v5/public/mark-price?instType=...|instId=...
  - WS public channel "mark-price"

Применение для HFT:
  - mark-price используется при расчёте unrealized PnL и margin ratio;
  - резкое расхождение (last - mark) сигнализирует о торгуемой
    дисперсии (premium/discount к индексу).
*/

package types

import "github.com/shopspring/decimal"

// MarkPrice — snapshot mark-цены.
type MarkPrice struct {
	// InstType — тип инструмента (SWAP/FUTURES/OPTION).
	InstType InstType
	// InstID — идентификатор инструмента.
	InstID string
	// MarkPx — текущая mark-цена (markPx).
	MarkPx decimal.Decimal
	// Ts — таймштамп snapshot'а в мс (ts).
	Ts int64
}

// MarkPrices — слайс mark-цен.
type MarkPrices []MarkPrice
