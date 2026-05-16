/*
ФАЙЛ: types/candle.go

ОПИСАНИЕ:
Структуры исторической свечи. Маппятся из массива чисел/строк ответа
`GET /api/v5/market/candles` и `history-candles`.

Формат ответа OKX (массив строк по позициям):

	[ ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm ]

Где confirm — флаг закрытия свечи ("0" — текущая, "1" — закрытая).

ОТЛИЧИЯ В СЕМАНТИКЕ Volume МЕЖДУ ПРОФИЛЯМИ — задача адаптера, не структуры:
  - spot:  Volume в base currency напрямую;
  - swap:  Volume в контрактах (умножать на ctVal для приведения в base).

Структура хранит обе цифры (Volume и VolumeQuote) и оставляет смысл единицы
на совесть вызывающего слоя.
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
