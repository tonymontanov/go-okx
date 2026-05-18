/*
ФАЙЛ: types/index-ticker.go

ОПИСАНИЕ:
IndexTicker — индексная цена и 24h метрики по индексу OKX (например
BTC-USDT-INDEX, ETH-USDT-INDEX). Используется как reference для perp,
delivery и опционных продуктов.

Маппится из:
  - GET /api/v5/market/index-tickers?quoteCcy=...|instId=...
  - WS public channel "index-tickers"

В отличие от обычного тикера здесь нет ask/bid/last — индекс это
расчётная величина, а не торгуемый инструмент.
*/

package types

import "github.com/shopspring/decimal"

// IndexTicker — snapshot индексной цены.
type IndexTicker struct {
	// InstID — идентификатор индекса (например "BTC-USDT-INDEX").
	InstID string
	// IdxPx — текущая индексная цена (idxPx).
	IdxPx decimal.Decimal
	// Open24h — индексная цена 24h назад (open24h).
	Open24h decimal.Decimal
	// High24h — максимальная индексная цена за 24h (high24h).
	High24h decimal.Decimal
	// Low24h — минимальная индексная цена за 24h (low24h).
	Low24h decimal.Decimal
	// SodUtc0 — индекс на 00:00 UTC (sodUtc0).
	SodUtc0 decimal.Decimal
	// SodUtc8 — индекс на 08:00 UTC (sodUtc8).
	SodUtc8 decimal.Decimal
	// Ts — таймштамп в мс (ts).
	Ts int64
}

// IndexTickers — слайс индексных тикеров.
type IndexTickers []IndexTicker
