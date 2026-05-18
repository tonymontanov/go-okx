/*
ФАЙЛ: types/estimated-price.go

ОПИСАНИЕ:
EstimatedPrice — ожидаемая цена delivery/exercise для FUTURES (delivery
price на момент экспирации) или OPTION (settlement price). Доступна
только в окне ~1 час до экспирации.

Маппится из:
  - GET /api/v5/public/estimated-price?instId=...

Не применима к SPOT/SWAP (у perp нет экспирации).
*/

package types

import "github.com/shopspring/decimal"

// EstimatedPrice — ожидаемая delivery/exercise цена.
type EstimatedPrice struct {
	// InstType — тип инструмента (FUTURES/OPTION).
	InstType InstType
	// InstID — идентификатор инструмента.
	InstID string
	// SettlePx — ожидаемая цена расчёта (settlePx).
	SettlePx decimal.Decimal
	// Ts — таймштамп расчёта в мс (ts).
	Ts int64
}
