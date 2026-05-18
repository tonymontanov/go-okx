/*
ФАЙЛ: types/funding-rate.go

ОПИСАНИЕ:
FundingRate — текущая funding-ставка perp и метаданные следующего
расчёта. FundingRateRecord — историческая запись (после settlement,
с realized rate).

Маппятся из:
  - GET /api/v5/public/funding-rate?instId=... → FundingRate
  - GET /api/v5/public/funding-rate-history?instId=... → FundingRateRecord
  - WS public channel "funding-rate" → FundingRate

ВАЖНО О ЗНАКАХ:
FundingRate > 0 ⇒ longs платят shorts (рынок bullish, perp выше index).
FundingRate < 0 ⇒ shorts платят longs (рынок bearish, perp ниже index).
Funding взимается каждые 8 часов (или 4 — зависит от инструмента),
точная периодичность доступна в SymbolInfo.fundingInterval (отдельно).

HFT-применение:
  - basis-trading (funding-arbitrage): подбираем сторону под знак;
  - предиктивный сигнал: NextFundingRate за ~30 мин до settlement часто
    стабилизируется и даёт оценку реального funding.
*/

package types

import "github.com/shopspring/decimal"

// FundingRate — текущая funding-ставка perp.
type FundingRate struct {
	// InstType — тип инструмента (всегда SWAP).
	InstType InstType
	// InstID — идентификатор perp-инструмента.
	InstID string
	// FundingRate — текущая ставка для применения на следующем settlement.
	FundingRate decimal.Decimal
	// NextFundingRate — оценка ставки на следующее окно после ближайшего
	// settlement (nextFundingRate).
	NextFundingRate decimal.Decimal
	// FundingTime — время следующего settlement в мс (fundingTime).
	FundingTime int64
	// NextFundingTime — время settlement после ближайшего (nextFundingTime).
	NextFundingTime int64
	// Method — метод расчёта ставки ("current_period" или "next_period").
	Method string
	// MinFundingRate — нижняя граница (minFundingRate).
	MinFundingRate decimal.Decimal
	// MaxFundingRate — верхняя граница (maxFundingRate).
	MaxFundingRate decimal.Decimal
	// SettleState — состояние расчёта (settState: "processing"/"settled").
	SettleState string
	// SettleFundingRate — финальная realized ставка после settlement, если
	// applicable (settFundingRate).
	SettleFundingRate decimal.Decimal
	// Premium — премия perp к индексу (premium).
	Premium decimal.Decimal
	// ImpactValue — impact notional value, используемый в расчёте funding
	// (impactValueOnPosition, в quote ccy).
	ImpactValue decimal.Decimal
	// Ts — таймштамп snapshot'а в мс (ts).
	Ts int64
}

// FundingRateRecord — историческая запись funding после settlement.
type FundingRateRecord struct {
	// InstType — всегда SWAP.
	InstType InstType
	// InstID — perp-инструмент.
	InstID string
	// FundingRate — ставка, объявленная перед settlement (fundingRate).
	FundingRate decimal.Decimal
	// RealizedRate — фактически реализованная ставка после settlement
	// (realizedRate). Может отличаться от FundingRate из-за капов.
	RealizedRate decimal.Decimal
	// FundingTime — момент settlement в мс (fundingTime).
	FundingTime int64
	// Method — метод расчёта.
	Method string
	// FormulaType — версия формулы ("noRate" / "withRate").
	FormulaType string
}

// FundingRates — слайс текущих funding-ставок.
type FundingRates []FundingRate

// FundingRateRecords — слайс исторических записей.
type FundingRateRecords []FundingRateRecord
