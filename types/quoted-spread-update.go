/*
ФАЙЛ: types/quoted-spread-update.go

ОПИСАНИЕ:
Обновление best bid/ask (спред). Подаётся в callback подписки WatchSpread.
Совпадает по смыслу с core/types.QuotedSpreadUpdate.

Формат канала bbo-tbt одинаков для spot и swap у OKX v5.

ПОЛЯ:
  - InstID    — инструмент.
  - BestBid   — лучшая цена покупки.
  - BestBidSz — объём на лучшем биде.
  - BestAsk   — лучшая цена продажи.
  - BestAskSz — объём на лучшем аске.
  - Ts        — таймштамп OKX (мс).
*/

package types

import "github.com/shopspring/decimal"

// QuotedSpreadUpdate — обновление best bid/ask.
type QuotedSpreadUpdate struct {
	InstID    string
	BestBid   decimal.Decimal
	BestBidSz decimal.Decimal
	BestAsk   decimal.Decimal
	BestAskSz decimal.Decimal
	Ts        int64
}
