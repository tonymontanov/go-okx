/*
FILE: spot/types/order-info.go

DESCRIPTION:
SPOT order information. Structurally matches SWAP OrderInfo
(see swaptypes.OrderInfo) — the only difference: on SPOT Size is in BASE
currency, not in contracts. A separate type is defined to ensure the Size
field documentation is accurate.
*/

package types

import "github.com/shopspring/decimal"

// OrderInfo — SPOT order information.
type OrderInfo struct {
	// OrderID — exchange order id (ordId).
	OrderID string
	// ClientOrderID — client order id (clOrdId).
	ClientOrderID string
	// InstID — instrument in OKX SPOT format (e.g. "BTC-USDT").
	InstID string
	// Side — order direction.
	Side SideType
	// OrderType — order type.
	OrderType OrderType
	// Price — order price.
	Price decimal.Decimal
	// Size — size in BASE currency (e.g. 0.1 BTC).
	Size decimal.Decimal
	// FilledSize — filled quantity (accFillSz), in BASE currency.
	FilledSize decimal.Decimal
	// State — order status.
	State OrderState
	// CreatedAtMs — creation timestamp (milliseconds).
	CreatedAtMs int64
	// UpdatedAtMs — last update timestamp.
	UpdatedAtMs int64
	// RateLimits — snapshot of rate-limit headers received with the response.
	RateLimits map[string]string

	// --- Per-update execution fields of the WS "orders" channel ------------------
	// Filled only by Stream().WatchOpenOrders / WatchOpenOrdersWithReset; REST
	// responses leave them zero. Every field describes the CURRENT push only
	// (OKX: "for the current update"), not a sticky "last fill": a push without a
	// trade (place, amend, cancel) carries FillSize = 0 and TradeID = "".

	// FillSize — quantity filled by this update (fillSz), in BASE currency.
	// Zero when the update is not a trade.
	FillSize decimal.Decimal
	// FillPrice — price of this update's fill (fillPx); zero when not a trade.
	FillPrice decimal.Decimal
	// TradeID — exchange trade id of this update's fill (tradeId); "" when not a trade.
	TradeID string
	// ExecType — liquidity role of this update's fill (execType): "T" taker, "M" maker.
	ExecType string
	// FillTimeMs — fill timestamp of this update in ms (fillTime); 0 when not a trade.
	FillTimeMs int64
	// FillFee — fee of this update's fill (fillFee): negative = fee charged,
	// positive = rebate. Zero when not a trade.
	FillFee decimal.Decimal
	// FillFeeCcy — currency of FillFee (fillFeeCcy).
	FillFeeCcy string
	// FillPnl — realised PnL of this update's fill (fillPnl); always 0 on cash spot.
	FillPnl decimal.Decimal
	// AmendResult — result of an amendment (amendResult): "" when the update is
	// not an amendment; "0" success, "-1" failure, "1" automatic cancel after a
	// failed amendment (cxlOnFail), "2" automatic amendment (options only).
	AmendResult string
	// CancelSource — source of a cancellation (cancelSource): "" when the update
	// is not a cancellation; "0" system, "1" user, "13" FOK not fully filled,
	// "14" IOC remainder canceled, "31" post-only would take liquidity, "32" self
	// trade prevention, ... (full list in the OKX docs, WS Order channel).
	CancelSource string
}
