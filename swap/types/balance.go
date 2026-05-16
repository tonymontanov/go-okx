/*
ФАЙЛ: swap/types/balance.go

ОПИСАНИЕ:
Доменная модель ответа GET /api/v5/account/balance.

OKX отдаёт один «account-summary» объект на запрос (даже массивом из одного
элемента — это просто формат REST). Внутри лежит per-currency массив `details`,
с балансом и маржой в каждой валюте.

Какие поля стороны (HFT-core) ждут от баланса:
  - Top-level: TotalEquityUSD, AdjustedEquityUSD (effective equity для маржи
    cross-режима), IsolatedEquityUSD, MarginRatio, NotionalUSD.
  - Per-currency: AvailableEquity (для cross-маржи и расчёта sizing), Equity,
    CashBalance, FrozenBalance, OrderFrozen, UnrealizedPnL.

ВНИМАНИЕ к деноминации:
  - totalEq / adjEq / isoEq — в USD (равноценный USD у unified-account).
  - eq / cashBal / availEq / availBal — В ВАЛЮТЕ ccy (USDT, BTC, ...).
  - eqUsd — пересчёт ccy-equity в USD (для удобства агрегаций).
  - mgnRatio: «безопасное» значение OKX обычно > 1; чем меньше, тем ближе к
    ликвидации (см. OKX docs Margin ratio).
*/

package types

import "github.com/shopspring/decimal"

// Balance — состояние unified-account целиком (топ-уровень + per-currency).
type Balance struct {
	// TotalEquityUSD — суммарный equity аккаунта в USD (totalEq).
	TotalEquityUSD decimal.Decimal
	// AdjustedEquityUSD — effective/adjusted equity, использованный OKX как
	// числитель margin ratio (adjEq). Для cross-аккаунта это «реальная»
	// маржа с учётом дисконтов на инструменты.
	AdjustedEquityUSD decimal.Decimal
	// IsolatedEquityUSD — equity, заблокированный в isolated-маржинальных
	// позициях (isoEq).
	IsolatedEquityUSD decimal.Decimal
	// OrderFrozenUSD — маржа, замороженная под pending cross-ордера (ordFroz).
	OrderFrozenUSD decimal.Decimal
	// InitialMarginUSD — initial margin requirement по аккаунту (imr).
	InitialMarginUSD decimal.Decimal
	// MaintenanceMarginUSD — maintenance margin requirement по аккаунту (mmr).
	MaintenanceMarginUSD decimal.Decimal
	// MarginRatio — margin ratio аккаунта (mgnRatio). У OKX это
	// adjEq / mmr; >1 безопасно, <1 — близко к ликвидации.
	MarginRatio decimal.Decimal
	// NotionalUSD — суммарная номинальная стоимость открытых позиций в USD.
	NotionalUSD decimal.Decimal
	// UpdatedAtMs — таймштамп последнего обновления (uTime).
	UpdatedAtMs int64
	// Details — балансы по каждой валюте.
	Details []BalanceDetail
}

// BalanceDetail — баланс и маржа для одной валюты внутри unified-account.
type BalanceDetail struct {
	// Ccy — тикер валюты (USDT, BTC, ETH, ...).
	Ccy string
	// Equity — equity в этой валюте (eq).
	Equity decimal.Decimal
	// CashBalance — кеш-баланс (cashBal). Чистый «положили на счёт минус
	// сняли», без учёта UPL.
	CashBalance decimal.Decimal
	// AvailableEquity — доступная маржа в этой валюте для открытия позиций
	// в cross-режиме (availEq). Базовый показатель для расчёта sizing.
	AvailableEquity decimal.Decimal
	// AvailableBalance — баланс, доступный к выводу (availBal). НЕ совпадает
	// с AvailableEquity: учитывает только cash, не UPL.
	AvailableBalance decimal.Decimal
	// FrozenBalance — общий замороженный баланс (frozenBal).
	FrozenBalance decimal.Decimal
	// OrderFrozen — баланс, замороженный под открытые ордера (ordFrozen).
	OrderFrozen decimal.Decimal
	// UnrealizedPnL — нереализованный PnL по позициям в этой валюте (upl).
	UnrealizedPnL decimal.Decimal
	// IsolatedUnrealizedPnL — UPL только по isolated-позициям (isoUpl).
	IsolatedUnrealizedPnL decimal.Decimal
	// DiscountEquity — equity с применённым OKX-дисконтом, используется как
	// маржа в multi-currency сценариях (disEq).
	DiscountEquity decimal.Decimal
	// EquityUSD — equity, переведённый в USD по марк-цене (eqUsd).
	EquityUSD decimal.Decimal
	// MarginRatio — margin ratio именно для этой валюты (mgnRatio).
	MarginRatio decimal.Decimal
	// UpdatedAtMs — таймштамп последнего обновления per-currency (uTime).
	UpdatedAtMs int64
}
