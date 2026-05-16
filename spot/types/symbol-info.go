/*
ФАЙЛ: spot/types/symbol-info.go

ОПИСАНИЕ:
Спецификация SPOT-инструмента. Отличия от SWAP:
  - НЕТ CtVal/CtMult (на споте ордер измеряется в базовой валюте напрямую,
    нет понятия "контракт").
  - НЕТ MaxMarketSize (OKX для спота возвращает только MaxLmtSz / MaxLmtAmt
    для лимитных и max market amount/size — мы храним их явно).
  - ЕСТЬ MinSize в базовой валюте.

Источник: GET /api/v5/public/instruments?instType=SPOT.
*/

package types

import "github.com/shopspring/decimal"

// SymbolInfo — спецификация SPOT-инструмента.
type SymbolInfo struct {
	// InstID — id инструмента, "BTC-USDT".
	InstID string
	// BaseCcy — базовая валюта ("BTC").
	BaseCcy string
	// QuoteCcy — котировочная валюта ("USDT").
	QuoteCcy string
	// TickSize — минимальный шаг цены (например 0.1).
	TickSize decimal.Decimal
	// LotSize — минимальный шаг количества (минимальный инкремент sz, в base).
	LotSize decimal.Decimal
	// MinSize — минимальный размер ордера в базовой валюте.
	MinSize decimal.Decimal
	// MaxLimitSize — максимальный размер для limit-ордера в базовой валюте.
	MaxLimitSize decimal.Decimal
	// MaxMarketSize — максимальный размер для market-ордера в базовой валюте
	// (поле maxMktSz, для market BUY с tgtCcy=quote_ccy — лимит будет в
	// quote-валюте, см. OKX docs).
	MaxMarketSize decimal.Decimal
	// PricePrecision — количество знаков после запятой в TickSize.
	PricePrecision int32
	// QuantityPrecision — количество знаков после запятой в LotSize.
	QuantityPrecision int32
}
