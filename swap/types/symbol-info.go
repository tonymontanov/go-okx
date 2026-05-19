/*
FILE: swap/types/symbol-info.go

DESCRIPTION:
SWAP instrument specification struct (filters, precision, contract value).
Populated from `GET /api/v5/public/instruments?instType=SWAP`.

FIELDS:
  - InstID         — instrument id, "BTC-USDT-SWAP".
  - BaseCcy        — base currency ("BTC").
  - QuoteCcy       — quote currency ("USDT").
  - SettleCcy      — settlement currency (equals QuoteCcy for USD-M SWAP).
  - CtVal          — value of one contract in base currency (contract value).
  - CtMult         — contract multiplier (usually 1).
  - TickSize       — minimum price increment (e.g. 0.1).
  - LotSize        — minimum quantity increment (minimum sz increment).
  - MinSize        — minimum order size in contracts.
  - MaxLimitSize   — maximum size for a limit order.
  - MaxMarketSize  — maximum size for a market order.

DERIVED FIELDS (for compatibility with core/types.SymbolInfo):
  - PricePrecision    — number of decimal places in TickSize.
  - QuantityPrecision — number of decimal places in LotSize.

DEPENDENCIES:
  - github.com/shopspring/decimal: precise numeric fields.
*/

package types

import "github.com/shopspring/decimal"

// SymbolInfo — SWAP instrument specification.
type SymbolInfo struct {
	InstID            string
	BaseCcy           string
	QuoteCcy          string
	SettleCcy         string
	CtVal             decimal.Decimal
	CtMult            decimal.Decimal
	TickSize          decimal.Decimal
	LotSize           decimal.Decimal
	MinSize           decimal.Decimal
	MaxLimitSize      decimal.Decimal
	MaxMarketSize     decimal.Decimal
	PricePrecision    int32
	QuantityPrecision int32
}
