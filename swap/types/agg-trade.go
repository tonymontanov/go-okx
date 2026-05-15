/*
ФАЙЛ: swap/types/agg-trade.go

ОПИСАНИЕ:
Структура AggTrade — одна агрегированная сделка из потока WS `trades`.
OKX в отличие от Binance не делает явной агрегации, и в канале `trades`
шлёт каждую сделку (publicly visible). Для совместимости с core (где есть
тип AggTrade) мы используем то же имя, но с decimal-полями.

ПОЛЯ:
  - InstID       — инструмент.
  - TradeID      — уникальный id сделки.
  - Price        — цена.
  - Size         — объём.
  - Side         — направление taker (buy/sell).
  - IsBuyerMaker — true, если maker — покупатель (для compatibility с core/types).
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
