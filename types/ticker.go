/*
ФАЙЛ: types/ticker.go

ОПИСАНИЕ:
Доменная модель тикера: 24h snapshot по одному инструменту, с last-trade,
top-of-book и агрегированными OHLCV-метриками за сутки.

Маппится из ответа:
  - GET /api/v5/market/ticker?instId=...
  - GET /api/v5/market/tickers?instType=...
  - WS public channel "tickers"

ПОЛЯ:
Семантика сохраняет именование OKX (instId, last, askPx, bidPx, vol24h,
volCcy24h, sodUtc0/8), но в decimal-форме. Размерности:
  - Last, AskPx, BidPx, Open24h, High24h, Low24h, SodUtc0, SodUtc8 —
    цена в quote currency.
  - LastSz, AskSz, BidSz — размер в контрактах (SWAP/FUTURES) или базовой
    валюте (SPOT/MARGIN). Адаптер решает, нужно ли мультиплицировать на
    ctVal.
  - Vol24h — объём в контрактах (SWAP/FUTURES) или base ccy (SPOT).
  - VolCcy24h — объём в quote ccy (SPOT) или base ccy (SWAP/FUTURES).

ВНИМАНИЕ:
OKX считает 24h окно как rolling, а sodUtc0/sodUtc8 — как «open at start
of day» в указанной TZ. Для арбитража/премии полезно SodUtc8 (Asia TZ),
для отчётности — SodUtc0.
*/

package types

import "github.com/shopspring/decimal"

// Ticker — 24h snapshot тикера по инструменту.
type Ticker struct {
	// InstType — тип инструмента (SPOT/SWAP/FUTURES/OPTION/MARGIN).
	InstType InstType
	// InstID — идентификатор инструмента.
	InstID string
	// Last — цена последней сделки (last).
	Last decimal.Decimal
	// LastSz — размер последней сделки (lastSz).
	LastSz decimal.Decimal
	// AskPx — лучшая цена ask (askPx).
	AskPx decimal.Decimal
	// AskSz — размер на лучшем ask (askSz).
	AskSz decimal.Decimal
	// BidPx — лучшая цена bid (bidPx).
	BidPx decimal.Decimal
	// BidSz — размер на лучшем bid (bidSz).
	BidSz decimal.Decimal
	// Open24h — цена открытия за последние 24 часа (open24h).
	Open24h decimal.Decimal
	// High24h — максимальная цена за 24 часа (high24h).
	High24h decimal.Decimal
	// Low24h — минимальная цена за 24 часа (low24h).
	Low24h decimal.Decimal
	// Vol24h — объём за 24 часа в базовой валюте/контрактах (vol24h).
	Vol24h decimal.Decimal
	// VolCcy24h — объём за 24 часа в quote currency (volCcy24h).
	VolCcy24h decimal.Decimal
	// SodUtc0 — open price на 00:00 UTC текущих суток (sodUtc0).
	SodUtc0 decimal.Decimal
	// SodUtc8 — open price на 08:00 UTC (00:00 Asia/HK) (sodUtc8).
	SodUtc8 decimal.Decimal
	// Ts — таймштамп snapshot'а в миллисекундах (ts).
	Ts int64
}

// Tickers — слайс тикеров (для GET /api/v5/market/tickers).
type Tickers []Ticker
