/*
FILE: types/risk-state.go

DESCRIPTION:
RiskState — current state of the risk engine for an account in Portfolio Margin
(PM) and Multi-currency Margin (MMR) modes. Shows the ATS (auto-deleveraging)
state of the account and which positions are flagged for ATS.

Mapped from:
  - GET /api/v5/account/risk-state

Applicable only for AcctLv == 4 (PortfolioMargin). For other levels the
endpoint returns an empty result.
*/

package types

import "github.com/shopspring/decimal"

// RiskState — account risk-engine state.
type RiskState struct {
	// Ts — snapshot timestamp in ms (ts).
	Ts int64
	// AtsErr — flag indicating ATS is active (atsErr).
	AtsErr bool
	// CtPos — list of contract positions with risk parameters.
	CtPos []RiskPosition
	// SpotPos — list of spot positions with risk parameters.
	SpotPos []RiskPosition
	// PendingMgnLiability — total margin liability of pending orders
	// (pendingMgnLiability).
	PendingMgnLiability decimal.Decimal
}

// RiskPosition — one position in the risk-state breakdown.
type RiskPosition struct {
	// InstID — instrument identifier.
	InstID string
	// Ccy — currency (for SPOT).
	Ccy string
	// Pos — position size in contracts/base.
	Pos decimal.Decimal
	// NotionalUsd — notional value of the position in USD.
	NotionalUsd decimal.Decimal
}
