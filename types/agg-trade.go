/*
ФАЙЛ: types/agg-trade.go

ОПИСАНИЕ:
AggTrade — одна агрегированная сделка из потока WS-канала `trades`. OKX, в
отличие от Binance, не делает явной агрегации — в канале `trades` приходит
каждая publicly visible сделка. Для совместимости с core (где есть тип AggTrade)
мы используем то же имя, но с decimal-полями.

Формат канала trades идентичен для spot и swap у OKX v5 — отличается только
семантика Size (base ccy для spot / контракты для swap), это решает адаптер.

ПОЛЯ:
  - InstID       — инструмент.
  - TradeID      — уникальный id сделки.
  - Price        — цена сделки.
  - Size         — объём (base ccy для spot / контракты для swap).
  - Side         — направление taker (buy/sell).
  - IsBuyerMaker — true, если maker — покупатель (для compatibility с core).
  - Ts           — таймштамп (мс).
*/

package types

import "github.com/shopspring/decimal"

// AggTrade — одна сделка из потока trades.
type AggTrade struct {
	InstID       string
	TradeID      string
	Price        decimal.Decimal
	Size         decimal.Decimal
	Side         SideType
	IsBuyerMaker bool
	Ts           int64
}
