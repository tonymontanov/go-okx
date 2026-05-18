/*
ФАЙЛ: types/liquidation.go

ОПИСАНИЕ:
Liquidation — одна запись о ликвидации позиции на бирже.

Маппится из:
  - GET /api/v5/public/liquidation-orders?instType=...&...
  - WS public channel "liquidation-orders"

ПРИМЕЧАНИЕ ОБ АГРЕГАЦИИ:
В WS-канале OKX обычно агрегирует ликвидации по инструменту (envelope
содержит instId, далее массив details). На уровне SDK мы разворачиваем
в плоский []Liquidation и доставляем доменному слою.

HFT-применение:
  - cascading liquidation signal: серия Liquidation за короткий
    интервал → ожидание spike-volatility/reversal;
  - ставка против over-leveraged side: считаем баланс sell-vs-buy
    ликвидаций в окне.
*/

package types

import "github.com/shopspring/decimal"

// Liquidation — одна ликвидация.
type Liquidation struct {
	// InstType — тип инструмента (SWAP/FUTURES/MARGIN/OPTION).
	InstType InstType
	// InstID — идентификатор инструмента.
	InstID string
	// Ccy — валюта (для MARGIN; для остальных пустая).
	Ccy string
	// Side — сторона ликвидации taker (buy = ликвидируется short,
	// sell = ликвидируется long).
	Side SideType
	// PosSide — сторона позиции (long/short/net). Для one-way mode
	// будет net; для long-short mode — long или short.
	PosSide string
	// Sz — размер ликвидированной позиции в контрактах (sz).
	Sz decimal.Decimal
	// BankruptcyPx — bankruptcy price (bkPx), цена, при которой equity
	// позиции равно нулю.
	BankruptcyPx decimal.Decimal
	// BankruptcyLoss — суммарный loss, перекрытый insurance fund
	// (bkLoss). 0, если ликвидация прошла без bankruptcy.
	BankruptcyLoss decimal.Decimal
	// Ts — таймштамп ликвидации в мс (ts).
	Ts int64
}

// Liquidations — слайс ликвидаций.
type Liquidations []Liquidation
