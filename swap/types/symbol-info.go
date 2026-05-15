/*
ФАЙЛ: swap/types/symbol-info.go

ОПИСАНИЕ:
Структура с информацией о торговом инструменте SWAP (фильтры, precision,
contract value). Заполняется из ответа `GET /api/v5/public/instruments?instType=SWAP`.

ПОЛЯ:
  - InstID         — id инструмента, "BTC-USDT-SWAP".
  - BaseCcy        — базовая валюта ("BTC").
  - QuoteCcy       — котировочная валюта ("USDT").
  - SettleCcy      — валюта расчётов (для USD-M SWAP совпадает с QuoteCcy).
  - CtVal          — стоимость одного контракта в базовой валюте (contract value).
  - CtMult         — мультипликатор контракта (обычно 1).
  - TickSize       — минимальный шаг цены (например 0.1).
  - LotSize        — минимальный шаг количества (минимальный инкремент sz).
  - MinSize        — минимальный размер ордера в контрактах.
  - MaxLimitSize   — максимальный размер для limit-ордера.
  - MaxMarketSize  — максимальный размер для market-ордера.

ВЫЧИСЛИМЫЕ ПОЛЯ (для совместимости с core/types.SymbolInfo):
  - PricePrecision    — количество знаков после запятой в TickSize.
  - QuantityPrecision — количество знаков после запятой в LotSize.

ЗАВИСИМОСТИ:
- github.com/shopspring/decimal: точные числовые поля.
*/

package types

import "github.com/shopspring/decimal"

// SymbolInfo — спецификация SWAP-инструмента.
type SymbolInfo struct {
	InstID            string
	BaseCcy           string
	QuoteCcy          string
	SettleCcy         string
	CtVal             decimal.Decimal
	CtMult            decimal.Decimal
	TickSize          decimal.Decimal
	LotSize           decimal.Decimal
	MinSize           decimal.Decimal
	MaxLimitSize      decimal.Decimal
	MaxMarketSize     decimal.Decimal
	PricePrecision    int32
	QuantityPrecision int32
}
