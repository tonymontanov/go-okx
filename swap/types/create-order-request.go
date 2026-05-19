/*
FILE: swap/types/create-order-request.go

DESCRIPTION:
Order creation request struct for the OKX SWAP section. Kept as close as
possible to the desk's `connectors/types.CreateOrderRequest` so that the
OKXSwapsConnector adapter works without data loss, but extended with OKX-specific
fields (TdMode, PosSide, Ccy).

FIELDS:
  - InstID         — instrument in OKX format (e.g. "BTC-USDT-SWAP").
  - Side           — buy/sell.
  - OrderType      — limit/market/post_only/fok/ioc. If an explicit OrderType is
                     set, the TimeInForce field is IGNORED. This allows the core
                     adapter (which only has TIF) and native SDK users (which have
                     OrderType) to work correctly with the same struct.
  - TimeInForce    — TIF in core/Binance notation (GTC/IOC/FOK/GTX). Converted to
                     OrderType if OrderType is empty.
  - Size           — quantity in contracts (lotSize). decimal.Decimal for precision;
                     converted to a lossless string when building the request.
  - Price          — price (for limit/post_only/fok/ioc). Not needed for market.
  - ClientOrderID  — client id (1..32 chars [A-Za-z0-9], letters and digits only;
                     the exchange rejects underscores/hyphens/dots with code 51000).
  - ReduceOnly     — ReduceOnly flag. Also enabled automatically for OptimalLimitIOC.
  - TdMode         — margin mode. If empty — derived automatically (cross for a
                     regular order; see ResolveTdMode in swap/client).
  - PosSide        — position side. Empty → "net" in net-mode (default).
  - Ccy            — margin currency for cross/isolated. Empty → derived from instrument.
  - Tag            — broker tag (optional, for the OKX broker programme).

INVARIANTS:
  - For OrderType=="market" the Price field is ignored when building the request.
  - For OrderType=="post_only" the server will reject an order that would immediately
    fill as a taker; the SDK does not duplicate this check.
*/

package types

import "github.com/shopspring/decimal"

// CreateOrderRequest — SWAP order creation request.
type CreateOrderRequest struct {
	InstID        string
	Side          SideType
	OrderType     OrderType
	TimeInForce   TimeInForceType
	Size          decimal.Decimal
	Price         decimal.Decimal
	ClientOrderID string
	ReduceOnly    bool
	TdMode        TdMode
	PosSide       PosSide
	Ccy           string
	Tag           string
}
