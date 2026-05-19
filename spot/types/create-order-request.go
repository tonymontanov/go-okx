/*
FILE: spot/types/create-order-request.go

DESCRIPTION:
SPOT order creation request. Differences from the SWAP variant:
  - NO PosSide (spot has no hedge mode).
  - NO ReduceOnly (shorting is not possible on cash spot — nothing to "reduce").
  - TdMode defaults to `cash` (not `cross`).
  - Size — in BASE currency (e.g. 0.1 for 0.1 BTC), not in contracts.
  - For market BUY, OKX interprets `sz` as QUOTE currency by default
    (USDT for BTC-USDT). To buy exactly N units of base currency, set
    TgtCcy = TgtCcyBase.

INVARIANTS:
  - For OrderType=Market on BUY: either pass Size in quote currency and
    omit TgtCcy (OKX default behaviour), or pass TgtCcy=TgtCcyBase and
    Size in base currency.
  - For all other OrderTypes (limit/post_only/fok/ioc): Size in BASE
    currency, Price is required.
*/

package types

import "github.com/shopspring/decimal"

// TgtCcy — the currency OKX should use as the unit of measurement for Size
// in spot market orders. Default: buy = quote_ccy (USDT), sell = base_ccy (BTC).
// For limit orders the parameter is IGNORED — sz is always in base currency.
type TgtCcy string

const (
	// TgtCcyBase — Size is interpreted as a quantity in base currency (BTC).
	TgtCcyBase TgtCcy = "base_ccy"
	// TgtCcyQuote — Size is interpreted as an amount in quote currency (USDT).
	TgtCcyQuote TgtCcy = "quote_ccy"
)

// CreateOrderRequest — SPOT order creation request.
type CreateOrderRequest struct {
	// InstID — instrument in OKX SPOT format (e.g. "BTC-USDT").
	InstID string

	// Side — buy/sell.
	Side SideType

	// OrderType — limit/market/post_only/fok/ioc. OptimalLimitIOC is not
	// supported on SPOT by the exchange; the SDK does not validate this,
	// OKX will return an error. If an explicit OrderType is set, TimeInForce
	// is IGNORED.
	OrderType OrderType

	// TimeInForce — TIF in Binance-style notation (GTC/IOC/FOK/GTX). Converted
	// to OrderType when OrderType is empty.
	TimeInForce TimeInForceType

	// Size — quantity in BASE currency (e.g. 0.1 BTC). For market BUY may be
	// interpreted as quote currency depending on TgtCcy.
	Size decimal.Decimal

	// Price — price for limit orders. Ignored for market.
	Price decimal.Decimal

	// ClientOrderID — client order id (1..32 characters [A-Za-z0-9], letters
	// and digits only; underscores/hyphens/dots are rejected by the exchange
	// with code 51000).
	ClientOrderID string

	// TdMode — margin mode. If empty, defaults to TdModeCash (SPOT default).
	// For spot margin trading (cross/isolated), must be set explicitly.
	TdMode TdMode

	// TgtCcy — unit of measurement for Size in market orders. See the TgtCcy
	// type doc-comment. For limit orders the parameter is ignored (sz is always
	// base). If empty, the OKX default is used (buy=quote, sell=base).
	TgtCcy TgtCcy

	// Ccy — margin currency (for spot margin trading). Not needed on cash spot.
	Ccy string

	// Tag — broker tag (optional, for the OKX broker program).
	Tag string
}
