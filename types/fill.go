/*
ФАЙЛ: types/fill.go

ОПИСАНИЕ:
Fill — одно исполнение ордера (полное или частичное). В отличие от
OrderInfo (где видно состояние ордера: live/partially_filled/filled/...),
Fill — это запись об отдельном trade-event'е: «по такому-то ордеру в
такое-то время исполнилось N контрактов по цене P, комиссия F».

Маппится из:
  - GET /api/v5/trade/fills        — последние 3 дня
  - GET /api/v5/trade/fills-history — последние 3 месяца
  - WS private channel "fills"

ПОЧЕМУ ОТДЕЛЬНЫЙ КАНАЛ В ДОПОЛНЕНИЕ К "orders":
В канале "orders" приходят push'и при каждом изменении state ордера,
включая частичные исполнения. Но «fills» специализирован:
  - меньше латентность (отдельный pipe, без логики state-tracking);
  - чище модель для PnL/inventory updates;
  - содержит fillPnl и fillTime, которых нет в orders в той же
    нормализованной форме.

ВНИМАНИЕ К ЗНАКАМ:
  - FillSz всегда положительный, направление через Side.
  - FillPnl положительный — прибыль; отрицательный — убыток (включая
    funding-payment если был на момент закрытия).
  - Fee отрицательный — комиссия списана со счёта (taker);
    положительный — rebate (maker, при VIP-программе).

ВНИМАНИЕ К RAW/HISTORIC FILLS:
Для REST /api/v5/trade/fills-history есть запаздывание до ~2 минут от
момента сделки. Для realtime используйте WS "fills" или REST
/api/v5/trade/fills (быстрее).
*/

package types

import "github.com/shopspring/decimal"

// Fill — одно исполнение ордера.
type Fill struct {
	// InstType — тип инструмента.
	InstType InstType
	// InstID — идентификатор инструмента.
	InstID string
	// TradeID — id сделки на бирже (tradeId).
	TradeID string
	// OrdID — id ордера, к которому относится fill (ordId).
	OrdID string
	// ClOrdID — клиентский id ордера, если задавался (clOrdId).
	ClOrdID string
	// BillID — id записи в bills (billId).
	BillID string
	// Tag — пользовательский тэг ордера (tag).
	Tag string
	// FillPx — цена исполнения (fillPx).
	FillPx decimal.Decimal
	// FillSz — размер исполнения в контрактах/base (fillSz).
	FillSz decimal.Decimal
	// FillPxVol — цена исполнения в IV (только для опционов, fillPxVol).
	FillPxVol decimal.Decimal
	// FillPxUsd — цена исполнения в USD (для опционов, fillPxUsd).
	FillPxUsd decimal.Decimal
	// FillMarkVol — mark volatility на момент fill (для опционов).
	FillMarkVol decimal.Decimal
	// FillFwdPx — forward price на момент fill (для опционов).
	FillFwdPx decimal.Decimal
	// FillMarkPx — mark price на момент fill (fillMarkPx).
	FillMarkPx decimal.Decimal
	// Side — сторона ордера (buy/sell).
	Side SideType
	// PosSide — сторона позиции (long/short/net). Пустая для cash spot.
	PosSide string
	// ExecType — тип исполнения ("T" = taker, "M" = maker) (execType).
	ExecType string
	// FeeCcy — валюта комиссии (feeCcy).
	FeeCcy string
	// Fee — размер комиссии; отрицательное = списано, положительное =
	// rebate (fee).
	Fee decimal.Decimal
	// FillPnl — realized PnL по этому fill'у (fillPnl).
	FillPnl decimal.Decimal
	// FillTime — таймштамп исполнения в мс (fillTime).
	FillTime int64
	// Ts — таймштамп записи на сервере OKX в мс (ts).
	Ts int64
}

// Fills — слайс fills.
type Fills []Fill
