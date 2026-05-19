/*
FILE: types/bill.go

DESCRIPTION:
Bill — a single entry in the account operations ledger (account bills). This is
any movement of funds: trade fill, funding payment, margin transfer,
deposit/withdrawal, liquidation, ADL, fee rebate, etc.

Mapped from:
  - GET /api/v5/account/bills          — last 7 days
  - GET /api/v5/account/bills-archive  — last 3 months

DIFFERENCE FROM Fill:
Fill — a record of a single trade event for an order (price, sz, fee).
Bill — a record of a single balance change (of any origin).
One fill produces 1-2 bills (fee debit + equity change);
a funding payment does not create a fill, but does create a bill.

HFT usage:
  - PnL reconciliation: bills are the only complete source of truth for
    fund movements (including funding, fees, rebates);
  - audit-trail: bills-archive (3 months) is used for reporting.
*/

package types

import "github.com/shopspring/decimal"

// BillType — top-level bill record type (OKX type).
type BillType string

// billType categories — aliases for OKX numeric values (1..N).
// Documentation: https://www.okx.com/docs-v5/en/#trading-account-rest-api-get-bills-details-last-7-days
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

// Bill — account ledger entry.
type Bill struct {
	// BillID — unique record id (billId).
	BillID string
	// Type — operation category (type).
	Type BillType
	// SubType — detailed sub-type (subType), e.g. funding/realized PnL.
	SubType string
	// Ccy — operation currency (ccy).
	Ccy string
	// InstID — instrument associated with the operation (if applicable).
	InstID string
	// InstType — instrument type.
	InstType InstType
	// MgnMode — margin mode at the time of the operation (mgnMode).
	MgnMode string
	// BalChg — balance change (balChg). Positive = received, negative = sent.
	BalChg decimal.Decimal
	// BalAfter — balance after the operation (bal).
	BalAfter decimal.Decimal
	// PnL — realized PnL of the operation (pnl).
	PnL decimal.Decimal
	// Fee — commission (fee).
	Fee decimal.Decimal
	// Ts — operation timestamp in ms (ts).
	Ts int64
	// ExecType — for trade-bill: "T" taker / "M" maker.
	ExecType string
	// From — source sub-account (for transfer).
	From string
	// To — destination sub-account (for transfer).
	To string
	// Notes — text comment, if any.
	Notes string
}

// Bills — slice of records.
type Bills []Bill
