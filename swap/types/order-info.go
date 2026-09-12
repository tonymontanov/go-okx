/*
FILE: swap/types/order-info.go

DESCRIPTION:
SWAP order information struct. Used as:
  - return value for CreateOrder/ModifyOrder/CancelOrder/Batch*;
  - element in the GetOpenOrders list;
  - payload for WatchOpenOrders events.

FIELDS:
  - OrderID       — exchange order id (ordId).
  - ClientOrderID — client order id (clOrdId).
  - InstID        — instrument.
  - Side          — direction.
  - Price         — order price.
  - Size          — total size (sz, in contracts).
  - FilledSize    — how much has been filled (accFillSz).
  - State         — status (live/partially_filled/filled/canceled).
  - CreatedAtMs   — creation timestamp (cTime, milliseconds).
  - UpdatedAtMs   — last update timestamp (uTime).
  - RateLimits    — snapshot of rate-limit headers received with the response.
    map[header]value.

PER-UPDATE EXECUTION FIELDS (WS "orders" channel only):
  - FillSize/FillPrice/TradeID/ExecType/FillTimeMs/FillFee/FillFeeCcy/FillPnl
    — the fill delivered by THIS push (OKX: "for the current update"); zero/""
    in a push without a trade (place, amend, cancel). FillSize is in contracts.
  - AmendResult/CancelSource — "" unless the push is an amendment / a cancellation.
  REST responses leave all of them zero.

All decimal values are stored as decimal.Decimal — losslessly. The core adapter
converts them to float64 (.InexactFloat64()) on its side.
*/

package types

import "github.com/shopspring/decimal"

// OrderInfo — SWAP order information.
type OrderInfo struct {
	OrderID       string
	ClientOrderID string
	InstID        string
	Side          SideType
	OrderType     OrderType
	Price         decimal.Decimal
	Size          decimal.Decimal
	FilledSize    decimal.Decimal
	State         OrderState
	CreatedAtMs   int64
	UpdatedAtMs   int64
	RateLimits    map[string]string

	// --- Per-update execution fields of the WS "orders" channel ------------------
	// Filled only by Stream().WatchOpenOrders / WatchOpenOrdersWithReset; REST
	// responses leave them zero. Every field describes the CURRENT push only
	// (OKX: "for the current update"), not a sticky "last fill": a push without a
	// trade (place, amend, cancel) carries FillSize = 0 and TradeID = "".

	// FillSize — quantity filled by this update (fillSz), in contracts.
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
	// FillPnl — realised PnL of this update's fill (fillPnl), close-position
	// trades only; 0 otherwise.
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
