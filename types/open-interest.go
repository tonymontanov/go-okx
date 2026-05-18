/*
ФАЙЛ: types/open-interest.go

ОПИСАНИЕ:
OpenInterest — суммарный объём открытых позиций по инструменту, в трёх
эквивалентных деноминациях (контракты, базовая валюта, USD).

Маппится из:
  - GET /api/v5/public/open-interest?instType=...|instId=...
  - WS public channel "open-interest"

HFT-применение:
  - изменение OI совместно с движением цены — индикатор реальных
    позиционных потоков (vs noise);
  - резкий drop OI при движении цены чаще указывает на ликвидации,
    рост OI — на свежие позиции.
*/

package types

import "github.com/shopspring/decimal"

// OpenInterest — snapshot открытого интереса.
type OpenInterest struct {
	// InstType — тип инструмента (SWAP/FUTURES/OPTION).
	InstType InstType
	// InstID — идентификатор инструмента.
	InstID string
	// OI — open interest в контрактах (oi).
	OI decimal.Decimal
	// OICcy — open interest в base currency (oiCcy).
	OICcy decimal.Decimal
	// OIUsd — open interest в USD-эквиваленте (oiUsd).
	OIUsd decimal.Decimal
	// Ts — таймштамп snapshot'а в мс (ts).
	Ts int64
}

// OpenInterests — слайс.
type OpenInterests []OpenInterest
