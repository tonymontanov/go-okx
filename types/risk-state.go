/*
ФАЙЛ: types/risk-state.go

ОПИСАНИЕ:
RiskState — текущее состояние риск-движка для аккаунта в режимах
Portfolio Margin (PM) и Multi-currency Margin (MMR). Показывает, в
каком ATS (auto-deleveraging) состоянии находится аккаунт и какие
позиции попадают под ATS-флаг.

Маппится из:
  - GET /api/v5/account/risk-state

Применяется только для AcctLv == 4 (PortfolioMargin). Для других уровней
endpoint вернёт пустой результат.
*/

package types

import "github.com/shopspring/decimal"

// RiskState — состояние risk-engine аккаунта.
type RiskState struct {
	// Ts — таймштамп snapshot'а в мс (ts).
	Ts int64
	// AtsErr — флаг, что ATS включён (atsErr).
	AtsErr bool
	// CtPos — список contract-позиций с риск-параметрами.
	CtPos []RiskPosition
	// SpotPos — список spot-позиций с риск-параметрами.
	SpotPos []RiskPosition
	// PendingMgnLiability — суммарная margin-liability по pending ордерам
	// (pendingMgnLiability).
	PendingMgnLiability decimal.Decimal
}

// RiskPosition — одна позиция в risk-state breakdown.
type RiskPosition struct {
	// InstID — идентификатор инструмента.
	InstID string
	// Ccy — валюта (для SPOT).
	Ccy string
	// Pos — размер позиции в контрактах/base.
	Pos decimal.Decimal
	// NotionalUsd — notional значение позиции в USD.
	NotionalUsd decimal.Decimal
}
