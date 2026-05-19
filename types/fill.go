/*
FILE: types/fill.go

DESCRIPTION:
Fill — a single order execution (full or partial). Unlike OrderInfo (which
shows the order state: live/partially_filled/filled/...), a Fill is a record
of an individual trade event: "for this order at this time N contracts were
executed at price P, fee F".

Mapped from:
  - GET /api/v5/trade/fills         — last 3 days
  - GET /api/v5/trade/fills-history — last 3 months
  - WS private channel "fills"

WHY A SEPARATE CHANNEL IN ADDITION TO "orders":
The "orders" channel pushes on every order state change including partial fills.
But "fills" is more specialised:
  - lower latency (separate pipe, no state-tracking logic);
  - cleaner model for PnL/inventory updates;
  - contains fillPnl and fillTime which are not available in "orders" in the
    same normalised form.

NOTE ON SIGNS:
  - FillSz is always positive; direction is conveyed by Side.
  - FillPnl positive — profit; negative — loss (includes funding payment if
    one occurred at close time).
  - Fee negative — fee debited from the account (taker);
    positive — rebate (maker, under a VIP programme).

NOTE ON HISTORIC FILLS:
REST /api/v5/trade/fills-history has up to ~2 minutes delay from trade time.
For real-time use WS "fills" or REST /api/v5/trade/fills (faster).
*/

package types

import "github.com/shopspring/decimal"

// Fill — a single order execution.
type Fill struct {
	// InstType — instrument type.
	InstType InstType
	// InstID — instrument identifier.
	InstID string
	// TradeID — exchange trade id (tradeId).
	TradeID string
	// OrdID — order id this fill belongs to (ordId).
	OrdID string
	// ClOrdID — client order id, if set (clOrdId).
	ClOrdID string
	// BillID — bills record id (billId).
	BillID string
	// Tag — user-defined order tag (tag).
	Tag string
	// FillPx — execution price (fillPx).
	FillPx decimal.Decimal
	// FillSz — execution size in contracts/base (fillSz).
	FillSz decimal.Decimal
	// FillPxVol — execution price in IV (options only, fillPxVol).
	FillPxVol decimal.Decimal
	// FillPxUsd — execution price in USD (options, fillPxUsd).
	FillPxUsd decimal.Decimal
	// FillMarkVol — mark volatility at fill time (options).
	FillMarkVol decimal.Decimal
	// FillFwdPx — forward price at fill time (options).
	FillFwdPx decimal.Decimal
	// FillMarkPx — mark price at fill time (fillMarkPx).
	FillMarkPx decimal.Decimal
	// Side — order side (buy/sell).
	Side SideType
	// PosSide — position side (long/short/net). Empty for cash spot.
	PosSide string
	// ExecType — execution type ("T" = taker, "M" = maker) (execType).
	ExecType string
	// FeeCcy — fee currency (feeCcy).
	FeeCcy string
	// Fee — fee amount; negative = debited, positive = rebate (fee).
	Fee decimal.Decimal
	// FillPnl — realized PnL for this fill (fillPnl).
	FillPnl decimal.Decimal
	// FillTime — execution timestamp in ms (fillTime).
	FillTime int64
	// Ts — OKX server record timestamp in ms (ts).
	Ts int64
}

// Fills — slice of fills.
type Fills []Fill

// FillsQuery — query parameters for GET /api/v5/trade/fills and
// /api/v5/trade/fills-history. All fields are optional.
type FillsQuery struct {
	// InstID — filter by instrument.
	InstID string
	// OrdID — filter by a specific order. Useful for the GetFill convenience
	// method when only executions of one order are needed.
	OrdID string
	// After — pagination: return records OLDER than the given billId
	// (i.e. "next page backwards in time").
	After string
	// Before — pagination: return records NEWER than the given billId.
	Before string
	// BeginMs — lower bound of fill time in ms (inclusive).
	BeginMs int64
	// EndMs — upper bound of fill time in ms (inclusive).
	EndMs int64
	// Limit — max records in response. OKX cap = 100; 0 ⇒ server default.
	Limit int
}
