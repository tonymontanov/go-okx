/*
ФАЙЛ: types/order-book-level.go

ОПИСАНИЕ:
Уровень стакана (одна точка глубины: цена + объём). Используется в:
  - REST snapshot (GetOrderBook);
  - orderbook engine (snapshot + delta);
  - WS push (Watch* функции).

Формат уровня одинаков для spot и swap у OKX v5 (отличается только единица
измерения size: spot = base currency, swap = контракты — это уже забота
адаптера, не уровня структуры).

ПОЛЯ:
  - Price — цена уровня. decimal.Decimal — без потерь и сравним без epsilon-trick.
  - Size  — объём на этом уровне (base ccy для spot / контракты для swap).

ПРИМЕЧАНИЕ:
Поле `Orders` (число ордеров на уровне), которое OKX отдаёт в массиве, мы
сознательно ОПУСКАЕМ в публичной структуре — оно почти никем не используется
в реальной торговле, но удваивает размер структуры. При необходимости вернётся
отдельным расширенным типом OrderBookLevelDetailed.
*/

package types

import "github.com/shopspring/decimal"

// OrderBookLevel — один уровень стакана.
type OrderBookLevel struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}
