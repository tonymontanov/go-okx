/*
ФАЙЛ: swap/types/candle.go

ОПИСАНИЕ:
Структуры исторической свечи. Маппятся из массива чисел/строк ответа
`GET /api/v5/market/candles` и `history-candles`.

Формат ответа OKX (массив строк по позициям):
  [ ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm ]

Где confirm — флаг закрытия свечи ("0" — текущая, "1" — закрытая).

ПОЛЯ Candle:
  - OpenTimeMs  — таймштамп открытия свечи (мс).
  - Open/High/Low/Close — OHLC цены.
  - Volume      — объём в базовой валюте (base contract amount).
  - VolumeQuote — объём в котировочной валюте.
  - Closed      — true, если свеча уже закрыта (confirm == 1).
*/

package types

import "github.com/shopspring/decimal"

// Candle — одна свеча.
type Candle struct {
	OpenTimeMs  int64
	Open        decimal.Decimal
	High        decimal.Decimal
	Low         decimal.Decimal
	Close       decimal.Decimal
	Volume      decimal.Decimal
	VolumeQuote decimal.Decimal
	Closed      bool
}

// Candles — слайс свечей.
type Candles []Candle
