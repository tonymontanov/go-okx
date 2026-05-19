/*
FILE: types/funding-rate.go

DESCRIPTION:
FundingRate — current perp funding rate and metadata for the next settlement.
FundingRateRecord — historical record (after settlement, with realized rate).

Mapped from:
  - GET /api/v5/public/funding-rate?instId=...         → FundingRate
  - GET /api/v5/public/funding-rate-history?instId=... → FundingRateRecord
  - WS public channel "funding-rate"                   → FundingRate

NOTE ON SIGNS:
FundingRate > 0 ⇒ longs pay shorts (bullish market, perp above index).
FundingRate < 0 ⇒ shorts pay longs (bearish market, perp below index).
Funding is collected every 8 hours (or 4 — depends on the instrument);
the exact frequency is available in SymbolInfo.fundingInterval (separately).

HFT usage:
  - basis-trading (funding-arbitrage): choose the side based on the sign;
  - predictive signal: NextFundingRate ~30 min before settlement often
    stabilizes and gives an estimate of the actual funding.
*/

package types

import "github.com/shopspring/decimal"

// FundingRate — current perp funding rate.
type FundingRate struct {
	// InstType — instrument type (always SWAP).
	InstType InstType
	// InstID — perp instrument identifier.
	InstID string
	// FundingRate — current rate to be applied at the next settlement.
	FundingRate decimal.Decimal
	// NextFundingRate — estimated rate for the window after the next
	// settlement (nextFundingRate).
	NextFundingRate decimal.Decimal
	// FundingTime — next settlement time in ms (fundingTime).
	FundingTime int64
	// NextFundingTime — settlement time after the next one (nextFundingTime).
	NextFundingTime int64
	// Method — rate calculation method ("current_period" or "next_period").
	Method string
	// MinFundingRate — lower bound (minFundingRate).
	MinFundingRate decimal.Decimal
	// MaxFundingRate — upper bound (maxFundingRate).
	MaxFundingRate decimal.Decimal
	// SettleState — settlement state (settState: "processing"/"settled").
	SettleState string
	// SettleFundingRate — final realized rate after settlement, if
	// applicable (settFundingRate).
	SettleFundingRate decimal.Decimal
	// Premium — perp premium over the index (premium).
	Premium decimal.Decimal
	// ImpactValue — impact notional value used in funding calculation
	// (impactValueOnPosition, in quote ccy).
	ImpactValue decimal.Decimal
	// Ts — snapshot timestamp in ms (ts).
	Ts int64
}

// FundingRateRecord — historical funding record after settlement.
type FundingRateRecord struct {
	// InstType — always SWAP.
	InstType InstType
	// InstID — perp instrument.
	InstID string
	// FundingRate — rate announced before settlement (fundingRate).
	FundingRate decimal.Decimal
	// RealizedRate — actually realized rate after settlement
	// (realizedRate). May differ from FundingRate due to caps.
	RealizedRate decimal.Decimal
	// FundingTime — settlement moment in ms (fundingTime).
	FundingTime int64
	// Method — calculation method.
	Method string
	// FormulaType — formula version ("noRate" / "withRate").
	FormulaType string
}

// FundingRates — slice of current funding rates.
type FundingRates []FundingRate

// FundingRateRecords — slice of historical records.
type FundingRateRecords []FundingRateRecord
