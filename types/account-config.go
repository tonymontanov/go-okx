/*
FILE: types/account-config.go

DESCRIPTION:
AccountConfig — unified-account configuration: margin mode, position mode,
account level (Simple/Single-currency/Multi-currency/Portfolio),
auto-loan, IP whitelist, etc.

Mapped from:
  - GET /api/v5/account/config

HFT usage:
  - On startup, read AccountConfig and validate that AcctLv ≥ 2
    (required for cross-margin spot and UTA features);
  - PosMode determines which posSide values are valid in CreateOrder
    ("net" vs "long"/"short");
  - AutoLoan indicates whether margin borrowing is allowed without an explicit
    command (important for strategy risk management).
*/

package types

import "github.com/shopspring/decimal"

// AccountLevel — OKX account level.
type AccountLevel string

// Account levels (acctLv in OKX API).
const (
	// AccountLevelSimple — Simple mode: spot only, no margin.
	AccountLevelSimple AccountLevel = "1"
	// AccountLevelSingleCcyMargin — Single-currency margin.
	AccountLevelSingleCcyMargin AccountLevel = "2"
	// AccountLevelMultiCcyMargin — Multi-currency margin.
	AccountLevelMultiCcyMargin AccountLevel = "3"
	// AccountLevelPortfolioMargin — Portfolio margin (PM).
	AccountLevelPortfolioMargin AccountLevel = "4"
)

// GreeksType — options Greeks display format.
type GreeksType string

const (
	// GreeksTypePA — Greeks per Asset (Pa, Pb, Pg).
	GreeksTypePA GreeksType = "PA"
	// GreeksTypeBS — Greeks Black-Scholes (delta, gamma, vega, theta).
	GreeksTypeBS GreeksType = "BS"
)

// AccountConfig — account settings.
type AccountConfig struct {
	// UID — internal user id.
	UID string
	// MainUID — main account uid (for sub-account equals the main account uid).
	MainUID string
	// AcctLv — account level (see AccountLevel* constants).
	AcctLv AccountLevel
	// PosMode — position mode ("net_mode" / "long_short_mode") (posMode).
	PosMode string
	// AutoLoan — true if auto-borrow is allowed in margin/cross.
	AutoLoan bool
	// GreeksType — Greeks display format (greeksType).
	GreeksType GreeksType
	// Level — user VIP level on the exchange (lv: "Lv1"..."Lv8"+"VIP1"...).
	Level string
	// CtIsoMode — isolated mode for contracts ("automatic"/"autonomy").
	CtIsoMode string
	// MgnIsoMode — isolated mode for margin ("automatic"/"quick_margin").
	MgnIsoMode string
	// LiquidationGear — liquidation buffer for multi-currency margin
	// (liquidationGear).
	LiquidationGear decimal.Decimal
	// SpotOffsetType — spot-offset type for PM ("1"/"2"/"3") (spotOffsetType).
	SpotOffsetType string
	// EnableSpotBorrow — true if spot-borrow is enabled in PM mode.
	EnableSpotBorrow bool
	// SpotBorrowAutoRepay — true if auto-repay is enabled for spot-borrow.
	SpotBorrowAutoRepay bool
	// LabelEnabledTradingPair — list of enabled trading pairs (if restricted).
	LabelEnabledTradingPair []string
	// IPAddresses — IP whitelist for the API key.
	IPAddresses []string
}
