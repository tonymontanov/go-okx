/*
FILE: types/balance.go

DESCRIPTION:
Domain model for the GET /api/v5/account/balance response.

OKX returns one "account-summary" object per request (even as a single-element
array — that is just the REST format). Inside is a per-currency array `details`
with the balance and margin for each currency.

UNIFIED-ACCOUNT: OKX uses ONE account per user — spot and swap balances are
returned by the same endpoint. Therefore the Balance model is SHARED between
both profiles; here, in the common package, is its natural home.

Fields expected by the HFT core side:
  - Top-level: TotalEquityUSD, AdjustedEquityUSD (effective equity for cross-margin
    mode), IsolatedEquityUSD, MarginRatio, NotionalUSD.
  - Per-currency: AvailableEquity (for cross-margin and sizing calculations), Equity,
    CashBalance, FrozenBalance, OrderFrozen, UnrealizedPnL.

NOTE ON DENOMINATION:
  - totalEq / adjEq / isoEq — in USD (USD-equivalent for unified-account).
  - eq / cashBal / availEq / availBal — IN ccy currency (USDT, BTC, ...).
  - eqUsd — ccy-equity converted to USD (for aggregation convenience).
  - mgnRatio: OKX considers values > 1 safe; the lower it is, the closer to
    liquidation (see OKX docs Margin ratio).
*/

package types

import "github.com/shopspring/decimal"

// Balance — full unified-account state (top-level + per-currency).
type Balance struct {
	// TotalEquityUSD — total account equity in USD (totalEq).
	TotalEquityUSD decimal.Decimal
	// AdjustedEquityUSD — effective/adjusted equity used by OKX as the
	// numerator of the margin ratio (adjEq). For a cross account this is the
	// "real" margin including instrument-level discounts.
	AdjustedEquityUSD decimal.Decimal
	// IsolatedEquityUSD — equity locked in isolated-margin positions (isoEq).
	IsolatedEquityUSD decimal.Decimal
	// OrderFrozenUSD — margin frozen for pending cross orders (ordFroz).
	OrderFrozenUSD decimal.Decimal
	// InitialMarginUSD — account-level initial margin requirement (imr).
	InitialMarginUSD decimal.Decimal
	// MaintenanceMarginUSD — account-level maintenance margin requirement (mmr).
	MaintenanceMarginUSD decimal.Decimal
	// MarginRatio — account margin ratio (mgnRatio). OKX computes this as
	// adjEq / mmr; >1 is safe, <1 is close to liquidation.
	MarginRatio decimal.Decimal
	// NotionalUSD — total notional value of open positions in USD.
	NotionalUSD decimal.Decimal
	// UpdatedAtMs — last update timestamp (uTime).
	UpdatedAtMs int64
	// Details — per-currency balances.
	Details []BalanceDetail
}

// BalanceDetail — balance and margin for one currency within the unified-account.
type BalanceDetail struct {
	// Ccy — currency ticker (USDT, BTC, ETH, ...).
	Ccy string
	// Equity — equity in this currency (eq).
	Equity decimal.Decimal
	// CashBalance — cash balance (cashBal). Net of deposits minus withdrawals,
	// excluding UPL.
	CashBalance decimal.Decimal
	// AvailableEquity — available margin in this currency for opening positions
	// in cross mode (availEq). Primary metric for sizing calculations.
	AvailableEquity decimal.Decimal
	// AvailableBalance — balance available for withdrawal (availBal). NOT equal
	// to AvailableEquity: considers only cash, not UPL.
	AvailableBalance decimal.Decimal
	// FrozenBalance — total frozen balance (frozenBal).
	FrozenBalance decimal.Decimal
	// OrderFrozen — balance frozen for open orders (ordFrozen).
	OrderFrozen decimal.Decimal
	// UnrealizedPnL — unrealized PnL on positions in this currency (upl).
	UnrealizedPnL decimal.Decimal
	// IsolatedUnrealizedPnL — UPL from isolated positions only (isoUpl).
	IsolatedUnrealizedPnL decimal.Decimal
	// DiscountEquity — equity with OKX discount applied, used as margin in
	// multi-currency scenarios (disEq).
	DiscountEquity decimal.Decimal
	// EquityUSD — equity converted to USD at mark price (eqUsd).
	EquityUSD decimal.Decimal
	// MarginRatio — margin ratio for this specific currency (mgnRatio).
	MarginRatio decimal.Decimal
	// UpdatedAtMs — per-currency last update timestamp (uTime).
	UpdatedAtMs int64
}
