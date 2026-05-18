/*
ФАЙЛ: types/bill.go

ОПИСАНИЕ:
Bill — одна запись в журнале операций по аккаунту (account bills). Это
любое движение средств: trade fill, funding payment, margin transfer,
deposit/withdrawal, liquidation, ADL, fee rebate, и т.д.

Маппится из:
  - GET /api/v5/account/bills          — последние 7 дней
  - GET /api/v5/account/bills-archive  — последние 3 месяца

ОТЛИЧИЕ ОТ Fill:
Fill — это запись об одном trade-event ордера (price, sz, fee).
Bill — это запись об одном изменении баланса (любого происхождения).
Один fill порождает 1-2 bill'а (списание fee + изменение equity);
funding payment не создаёт fill, но создаёт bill.

HFT-применение:
  - PnL reconciliation: bills — единственный полный источник истины
    по движению средств (включая funding, fees, rebates);
  - audit-trail: для отчётности используется bills-archive (3 месяца).
*/

package types

import "github.com/shopspring/decimal"

// BillType — top-level тип записи bill (OKX type).
type BillType string

// Категории billType — синонимы числовых значений OKX (1..N).
// Документация: https://www.okx.com/docs-v5/en/#trading-account-rest-api-get-bills-details-last-7-days
const (
	BillTypeTransfer        BillType = "1"
	BillTypeTrade           BillType = "2"
	BillTypeDelivery        BillType = "3"
	BillTypeAutoTokenConv   BillType = "4"
	BillTypeLiquidation     BillType = "5"
	BillTypeMarginTransfer  BillType = "6"
	BillTypeInterestDeducted BillType = "7"
	BillTypeFunding         BillType = "8"
	BillTypeADL             BillType = "9"
	BillTypeClawback        BillType = "10"
	BillTypeSysToken        BillType = "11"
	BillTypeStrategyTrans   BillType = "12"
	BillTypeDDH             BillType = "13"
	BillTypeBlockTrade      BillType = "14"
	BillTypeQuickMargin     BillType = "15"
	BillTypeBorrow          BillType = "18"
	BillTypeRepay           BillType = "19"
	BillTypeAutoSubscribe   BillType = "22"
	BillTypeAutoRedeem      BillType = "23"
	BillTypeOTC             BillType = "24"
	BillTypeFee             BillType = "27"
)

// Bill — запись журнала аккаунта.
type Bill struct {
	// BillID — уникальный id записи (billId).
	BillID string
	// Type — категория операции (type).
	Type BillType
	// SubType — детализированный sub-type (subType), напр. funding/realized PnL.
	SubType string
	// Ccy — валюта операции (ccy).
	Ccy string
	// InstID — инструмент, к которому относится операция (если применимо).
	InstID string
	// InstType — тип инструмента.
	InstType InstType
	// MgnMode — режим маржи на момент операции (mgnMode).
	MgnMode string
	// BalChg — изменение баланса (balChg). Положительное = пришло,
	// отрицательное = ушло.
	BalChg decimal.Decimal
	// BalAfter — баланс после операции (bal).
	BalAfter decimal.Decimal
	// PnL — realized PnL по операции (pnl).
	PnL decimal.Decimal
	// Fee — комиссия (fee).
	Fee decimal.Decimal
	// Ts — таймштамп операции в мс (ts).
	Ts int64
	// ExecType — для trade-bill: "T" taker / "M" maker.
	ExecType string
	// From — sub-account источник (для transfer).
	From string
	// To — sub-account получатель (для transfer).
	To string
	// Notes — текстовый комментарий, если есть.
	Notes string
}

// Bills — слайс записей.
type Bills []Bill
