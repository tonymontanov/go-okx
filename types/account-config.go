/*
ФАЙЛ: types/account-config.go

ОПИСАНИЕ:
AccountConfig — конфигурация unified-account: режим маржи, режим позиций,
уровень аккаунта (Simple/Single-currency/Multi-currency/Portfolio),
auto-loan, IP-whitelist и т.п.

Маппится из:
  - GET /api/v5/account/config

HFT-применение:
  - На старте читаем AccountConfig и валидируем, что AcctLv ≥ 2
    (для cross-margin spot и UTA-фич);
  - PosMode определяет, какие значения posSide допустимы при
    CreateOrder ("net" vs "long"/"short");
  - AutoLoan показывает, разрешён ли margin-borrow без явной команды
    (важно для риск-менеджмента стратегий).
*/

package types

import "github.com/shopspring/decimal"

// AccountLevel — уровень аккаунта OKX.
type AccountLevel string

// Уровни аккаунта (acctLv в OKX API).
const (
	// AccountLevelSimple — Simple mode: только spot, без маржи.
	AccountLevelSimple AccountLevel = "1"
	// AccountLevelSingleCcyMargin — Single-currency margin.
	AccountLevelSingleCcyMargin AccountLevel = "2"
	// AccountLevelMultiCcyMargin — Multi-currency margin.
	AccountLevelMultiCcyMargin AccountLevel = "3"
	// AccountLevelPortfolioMargin — Portfolio margin (PM).
	AccountLevelPortfolioMargin AccountLevel = "4"
)

// GreeksType — формат отображения греков опционов.
type GreeksType string

const (
	// GreeksTypePA — Greeks per Asset (Pa, Pb, Pg).
	GreeksTypePA GreeksType = "PA"
	// GreeksTypeBS — Greeks Black-Scholes (delta, gamma, vega, theta).
	GreeksTypeBS GreeksType = "BS"
)

// AccountConfig — настройки аккаунта.
type AccountConfig struct {
	// UID — внутренний user id.
	UID string
	// MainUID — uid главного аккаунта (для sub-account == uid главного).
	MainUID string
	// AcctLv — уровень аккаунта (см. константы AccountLevel*).
	AcctLv AccountLevel
	// PosMode — режим позиций ("net_mode" / "long_short_mode") (posMode).
	PosMode string
	// AutoLoan — true, если разрешён auto-borrow в margin/cross.
	AutoLoan bool
	// GreeksType — формат отображения греков (greeksType).
	GreeksType GreeksType
	// Level — VIP-level пользователя на бирже (lv: "Lv1"..."Lv8"+"VIP1"...).
	Level string
	// CtIsoMode — режим isolated для contracts ("automatic"/"autonomy").
	CtIsoMode string
	// MgnIsoMode — режим isolated для margin ("automatic"/"quick_margin").
	MgnIsoMode string
	// LiquidationGear — buffer для ликвидации в multi-currency-margin
	// (liquidationGear).
	LiquidationGear decimal.Decimal
	// SpotOffsetType — тип spot-offset для PM ("1"/"2"/"3") (spotOffsetType).
	SpotOffsetType string
	// EnableSpotBorrow — true, если включён spot-borrow в режиме PM.
	EnableSpotBorrow bool
	// SpotBorrowAutoRepay — true, если включён auto-repay для spot-borrow.
	SpotBorrowAutoRepay bool
	// LabelEnabledTradingPair — список включённых торговых пар (если ограничено).
	LabelEnabledTradingPair []string
	// IPAddresses — IP-whitelist для API-ключа.
	IPAddresses []string
}
