/*
FILE: spot/types/symbol-info.go

DESCRIPTION:
SPOT instrument specification. Differences from SWAP:
  - NO CtVal/CtMult (on spot an order is measured directly in the base currency;
    the concept of "contract" does not exist).
  - NO MaxMarketSize (for spot OKX returns only MaxLmtSz / MaxLmtAmt for limit
    orders and max market amount/size — stored explicitly).
  - MinSize IS present, in base currency.

Source: GET /api/v5/public/instruments?instType=SPOT.
*/

package types

import "github.com/shopspring/decimal"

// SymbolInfo — SPOT instrument specification.
type SymbolInfo struct {
	// InstID — instrument id, "BTC-USDT".
	InstID string
	// BaseCcy — base currency ("BTC").
	BaseCcy string
	// QuoteCcy — quote currency ("USDT").
	QuoteCcy string
	// TickSize — minimum price increment (e.g. 0.1).
	TickSize decimal.Decimal
	// LotSize — minimum quantity increment (minimum sz increment, in base).
	LotSize decimal.Decimal
	// MinSize — minimum order size in base currency.
	MinSize decimal.Decimal
	// MaxLimitSize — maximum size for a limit order in base currency.
	MaxLimitSize decimal.Decimal
	// MaxMarketSize — maximum size for a market order in base currency
	// (field maxMktSz; for market BUY with tgtCcy=quote_ccy the limit
	// is in quote currency — see OKX docs).
	MaxMarketSize decimal.Decimal
	// PricePrecision — number of decimal places in TickSize.
	PricePrecision int32
	// QuantityPrecision — number of decimal places in LotSize.
	QuantityPrecision int32
}
