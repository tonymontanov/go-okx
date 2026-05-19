/*
FILE: spot/stream.go

DESCRIPTION:
Domain sub-client for SPOT WebSocket subscriptions. Built on top of internal/ws.Conn
with the same patterns as swap/stream.go:
  - one public-conn for the entire spot client (books, bbo-tbt, trades);
  - one private-conn (orders, account);
  - reconnect/relogin/resubscribe are transparent to the caller;
  - local parse errors — log+drop.

OKX CHANNELS FOR SPOT (supported):
  - "books"      : full order book (same protocol with checksum/seqId as SWAP).
  - "bbo-tbt"    : best bid/ask, tick-by-tick.
  - "trades"     : trades.
  - "orders"     : private, InstType="SPOT" to filter spot orders only.
  - "account"    : private, unified-account balance updates (shared with SWAP).

NOT PRESENT (compared to swap/stream.go):
  - mark-price, index-tickers, positions — these channels do not exist / are not needed for SPOT.
*/

package spot

import (
	"context"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/ws"
	"github.com/tonymontanov/go-okx/v2/orderbook"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

// StreamClient — SPOT WS subscriptions sub-client.
type StreamClient struct {
	c *Client
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{c: c}
}

// ----------------------------------------------------------------------------
// PUBLIC streams
// ----------------------------------------------------------------------------

// rawBookPush — push data for the books channel.
type rawBookPush struct {
	Asks      [][]string `json:"asks"`
	Bids      [][]string `json:"bids"`
	Ts        string     `json:"ts"`
	Checksum  int32      `json:"checksum"`
	SeqID     int64      `json:"seqId"`
	PrevSeqID int64      `json:"prevSeqId"`
}

/*
WatchOrderbook subscribes to the books channel and maintains a local order book
via orderbook.Engine. Semantics are identical to swap.WatchOrderbook — the books
channel format is the same for SPOT and SWAP in OKX v5.
*/
func (s *StreamClient) WatchOrderbook(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books", instID, depth, handler, errHandler)
}

/*
WatchOrderbookL2Tbt subscribes to the books-l2-tbt channel: FULL L2 order book
tick-by-tick (push on every change, no 100ms batching). The message format is
identical to "books" (snapshot+update+checksum+seqId), so the same orderbook.Engine
is used.

OKX REQUIREMENTS: channel available to VIP4+ or Market Maker only. Lower-tier
accounts will receive an error-event with code 60018.

WHEN TO USE:
  - strategies with very short quoting intervals (≤10 ms) that need to catch
    intermediate top-of-book changes;
  - latency arbitrage and cross-exchange MM.

WHEN NOT TO USE:
  - the channel is much "noisier" (higher traffic volume and CPU for parsing).
    For a typical MM strategy "books" with 100ms batching is sufficient.
*/
func (s *StreamClient) WatchOrderbookL2Tbt(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books-l2-tbt", instID, depth, handler, errHandler)
}

/*
WatchOrderbookBooks50L2Tbt subscribes to the books50-l2-tbt channel: top-50
L2 order book tick-by-tick. Intermediate in requirements and load between
"books" and "books-l2-tbt".

OKX REQUIREMENTS: VIP2+ or Market Maker.
*/
func (s *StreamClient) WatchOrderbookBooks50L2Tbt(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books50-l2-tbt", instID, depth, handler, errHandler)
}

/*
WatchOrderbookBooks5 subscribes to the books5 channel: top-5 levels,
batched at 100ms. Unlike other book channels, each push is a FULL snapshot
(no incremental updates or checksums). Therefore orderbook.Engine is not
needed — the handler receives the snapshot directly.

WHEN TO USE:
  - the strategy uses top-of-book only (top-5 is more than enough);
  - consistency guarantees against the exchange are not required (snapshot is self-contained);
  - minimal parsing overhead and zero client-side state are desired.

LIMITATIONS:
  - depth parameter is ignored (always 5);
  - SeqID/Checksum are not populated in the snapshot.
*/
func (s *StreamClient) WatchOrderbookBooks5(
	ctx context.Context, instID string,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "stream.WatchOrderbookBooks5: InstID is empty", nil)
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel: "books5",
		InstID:  instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawBookPush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchOrderbookBooks5: parse", okx.Str("instId", instID), okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				handler(types.OrderBookSnapshot{
					InstID: instID,
					Bids:   parseBookLevels(pushes[i].Bids),
					Asks:   parseBookLevels(pushes[i].Asks),
				})
			}
		},
	}
	s.c.publicConn().Start(ctx)
	if err := s.c.publicConn().Subscribe(sub); err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

// watchBookEngine — shared implementation for all L2 channels with incremental
// updates (books, books-l2-tbt, books50-l2-tbt). One message format, one engine,
// one logic — only the channel name differs.
func (s *StreamClient) watchBookEngine(
	ctx context.Context, channel, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "stream.WatchOrderbook("+channel+"): InstID is empty", nil)
	}
	if depth <= 0 {
		depth = 25
	}

	var cfg okx.Config = s.c.config()
	var eng *orderbook.Engine = orderbook.NewEngine(instID, cfg.Orderbook.MaxDepth, cfg.Orderbook.ChecksumLevels)

	var sub *ws.Subscription = &ws.Subscription{
		Channel: channel,
		InstID:  instID,
		Reset: func() {
			eng.MarkResynced(0, 0)
		},
		Handler: func(action string, payload []byte) {
			var pushes []rawBookPush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchOrderbook: parse", okx.Str("channel", channel), okx.Str("instId", instID), okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				var p rawBookPush = pushes[i]
				var ts int64
				ts, _ = codec.ParseInt64(p.Ts)
				if action == "snapshot" || (action == "" && i == 0) {
					eng.ApplySnapshot(orderbook.Snapshot{
						InstID:    instID,
						Bids:      parseBookLevels(p.Bids),
						Asks:      parseBookLevels(p.Asks),
						SeqID:     p.SeqID,
						PrevSeqID: p.PrevSeqID,
						Checksum:  p.Checksum,
						TsMs:      ts,
					})
				} else {
					eng.ApplyUpdate(orderbook.Update{
						InstID:    instID,
						Bids:      parseBookLevels(p.Bids),
						Asks:      parseBookLevels(p.Asks),
						SeqID:     p.SeqID,
						PrevSeqID: p.PrevSeqID,
						Checksum:  p.Checksum,
						TsMs:      ts,
					})
				}
			}
			if eng.IsDirty() {
				return
			}
			var bids, asks = eng.TopLevels(depth)
			handler(types.OrderBookSnapshot{
				InstID: instID,
				Bids:   bids,
				Asks:   asks,
				SeqID:  eng.LastSeqID(),
			})
		},
	}

	s.c.publicConn().Start(ctx)
	if err := s.c.publicConn().Subscribe(sub); err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

// parseBookLevels converts [["px","sz",...], ...] → []OrderBookLevel.
func parseBookLevels(raw [][]string) []types.OrderBookLevel {
	var out []types.OrderBookLevel = make([]types.OrderBookLevel, 0, len(raw))
	var i int
	for i = 0; i < len(raw); i++ {
		if len(raw[i]) < 2 {
			continue
		}
		var lvl types.OrderBookLevel
		var err error
		lvl.Price, err = codec.ParseDecimal(raw[i][0])
		if err != nil {
			continue
		}
		lvl.Size, err = codec.ParseDecimal(raw[i][1])
		if err != nil {
			continue
		}
		out = append(out, lvl)
	}
	return out
}

// rawBboPush — push data for the bbo-tbt channel.
type rawBboPush struct {
	Asks [][]string `json:"asks"`
	Bids [][]string `json:"bids"`
	Ts   string     `json:"ts"`
}

/*
WatchSpread subscribes to the bbo-tbt channel and delivers best bid/ask updates.
*/
func (s *StreamClient) WatchSpread(
	ctx context.Context, instID string,
	handler func(types.QuotedSpreadUpdate), errHandler func(error),
) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "stream.WatchSpread: InstID is empty", nil)
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel: "bbo-tbt",
		InstID:  instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawBboPush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchSpread: parse", okx.Str("instId", instID), okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				var p rawBboPush = pushes[i]
				if len(p.Bids) == 0 || len(p.Asks) == 0 {
					continue
				}
				if len(p.Bids[0]) < 2 || len(p.Asks[0]) < 2 {
					continue
				}
				var upd types.QuotedSpreadUpdate
				upd.InstID = instID
				upd.BestBid, _ = codec.ParseDecimal(p.Bids[0][0])
				upd.BestBidSz, _ = codec.ParseDecimal(p.Bids[0][1])
				upd.BestAsk, _ = codec.ParseDecimal(p.Asks[0][0])
				upd.BestAskSz, _ = codec.ParseDecimal(p.Asks[0][1])
				upd.Ts, _ = codec.ParseInt64(p.Ts)
				handler(upd)
			}
		},
	}
	s.c.publicConn().Start(ctx)
	if err := s.c.publicConn().Subscribe(sub); err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

// rawTradePush — push data for the trades channel.
type rawTradePush struct {
	InstID  string `json:"instId"`
	TradeID string `json:"tradeId"`
	Px      string `json:"px"`
	Sz      string `json:"sz"`
	Side    string `json:"side"`
	Ts      string `json:"ts"`
}

// WatchLastPrice — trades channel, delivers price and timestamp only.
func (s *StreamClient) WatchLastPrice(
	ctx context.Context, instID string,
	handler func(price float64, tsMs int64), errHandler func(error),
) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "stream.WatchLastPrice: InstID is empty", nil)
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel: "trades",
		InstID:  instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawTradePush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchLastPrice: parse", okx.Str("instId", instID), okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				var px float64
				var ts int64
				px, _ = codec.ParseFloat64(pushes[i].Px)
				ts, _ = codec.ParseInt64(pushes[i].Ts)
				handler(px, ts)
			}
		},
	}
	s.c.publicConn().Start(ctx)
	if err := s.c.publicConn().Subscribe(sub); err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

// WatchAggTrades — trades channel, full AggTrade.
func (s *StreamClient) WatchAggTrades(
	ctx context.Context, instID string,
	handler func(types.AggTrade), errHandler func(error),
) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "stream.WatchAggTrades: InstID is empty", nil)
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel: "trades",
		InstID:  instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawTradePush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchAggTrades: parse", okx.Str("instId", instID), okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				var p rawTradePush = pushes[i]
				var t types.AggTrade
				t.InstID = instID
				t.TradeID = p.TradeID
				t.Price, _ = codec.ParseDecimal(p.Px)
				t.Size, _ = codec.ParseDecimal(p.Sz)
				t.Side = types.SideType(p.Side)
				t.IsBuyerMaker = p.Side == "sell"
				t.Ts, _ = codec.ParseInt64(p.Ts)
				handler(t)
			}
		},
	}
	s.c.publicConn().Start(ctx)
	if err := s.c.publicConn().Subscribe(sub); err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

// ----------------------------------------------------------------------------
// PRIVATE streams
// ----------------------------------------------------------------------------

// rawOrderPush — push data for the orders channel.
type rawOrderPush struct {
	InstID    string `json:"instId"`
	OrdID     string `json:"ordId"`
	ClOrdID   string `json:"clOrdId"`
	OrdType   string `json:"ordType"`
	Side      string `json:"side"`
	Px        string `json:"px"`
	Sz        string `json:"sz"`
	AccFillSz string `json:"accFillSz"`
	State     string `json:"state"`
	CTime     string `json:"cTime"`
	UTime     string `json:"uTime"`
}

/*
WatchAccount — account channel (private). Each push message contains a
unified-account balance snapshot. Subscription lives until ctx is cancelled.

The channel is shared between SPOT and SWAP (unified-account), but exposing it
in the spot stream is important for applications that work with SPOT only and do
not want to initialise the SWAP client (saving one WS connection).
*/
func (s *StreamClient) WatchAccount(
	ctx context.Context,
	handler func(types.Balance), errHandler func(error),
) error {
	if !s.c.signerEnabled() {
		var err error = okx.NewError(okx.ErrorKindAuth, "", "stream.WatchAccount: credentials required", nil)
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel: "account",
		Handler: func(_ string, payload []byte) {
			var pushes []rawBalance
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchAccount: parse", okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				handler(convertBalance(pushes[i]))
			}
		},
	}
	s.c.privateConn().Start(ctx)
	if err := s.c.privateConn().Subscribe(sub); err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

/*
WatchOpenOrders — orders channel (private) filtered by InstType="SPOT".
The callback receives the list of orders delivered in a single push.
*/
func (s *StreamClient) WatchOpenOrders(
	ctx context.Context, instID string,
	handler func([]types.OrderInfo), errHandler func(error),
) error {
	if !s.c.signerEnabled() {
		var err error = okx.NewError(okx.ErrorKindAuth, "", "stream.WatchOpenOrders: credentials required", nil)
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel:  "orders",
		InstType: "SPOT",
		InstID:   instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawOrderPush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchOpenOrders: parse", okx.Err(err))
				return
			}
			var out []types.OrderInfo = make([]types.OrderInfo, 0, len(pushes))
			var i int
			for i = 0; i < len(pushes); i++ {
				var p rawOrderPush = pushes[i]
				if instID != "" && p.InstID != instID {
					continue
				}
				var info types.OrderInfo
				info.OrderID = p.OrdID
				info.ClientOrderID = p.ClOrdID
				info.InstID = p.InstID
				info.Side = types.SideType(p.Side)
				info.OrderType = types.OrderType(p.OrdType)
				info.State = types.ParseOrderState(p.State)
				info.Price, _ = codec.ParseDecimal(p.Px)
				info.Size, _ = codec.ParseDecimal(p.Sz)
				info.FilledSize, _ = codec.ParseDecimal(p.AccFillSz)
				info.CreatedAtMs, _ = codec.ParseInt64(p.CTime)
				info.UpdatedAtMs, _ = codec.ParseInt64(p.UTime)
				out = append(out, info)
			}
			if len(out) > 0 {
				handler(out)
			}
		},
	}
	s.c.privateConn().Start(ctx)
	if err := s.c.privateConn().Subscribe(sub); err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

// rawFillPush — push data for the fills channel.
type rawFillPush struct {
	InstType    string `json:"instType"`
	InstID      string `json:"instId"`
	TradeID     string `json:"tradeId"`
	OrdID       string `json:"ordId"`
	ClOrdID     string `json:"clOrdId"`
	BillID      string `json:"billId"`
	Tag         string `json:"tag"`
	FillPx      string `json:"fillPx"`
	FillSz      string `json:"fillSz"`
	FillPxVol   string `json:"fillPxVol"`
	FillPxUsd   string `json:"fillPxUsd"`
	FillMarkVol string `json:"fillMarkVol"`
	FillFwdPx   string `json:"fillFwdPx"`
	FillMarkPx  string `json:"fillMarkPx"`
	Side        string `json:"side"`
	PosSide     string `json:"posSide"`
	ExecType    string `json:"execType"`
	FeeCcy      string `json:"feeCcy"`
	Fee         string `json:"fee"`
	FillPnl     string `json:"fillPnl"`
	FillTime    string `json:"fillTime"`
	Ts          string `json:"ts"`
}

/*
WatchFills — private "fills" channel filtered by InstType="SPOT". Pushes
on every execution (full or partial) with the minimum exchange latency —
LOWER than the "orders" channel, where executions appear as part of the
order state machine.

OKX REQUIREMENTS: VIP5+ or Market Maker. Otherwise the server responds 60018.

ADVANTAGES:
  - lower fill-event latency vs parsing "orders";
  - no noisy intermediate state-updates (live → live amend → ...);
  - fillPnl and execType (T/M) are provided ready to use — no extra
    round-trip GetFill is needed.

PARAMETERS:
  - instID is optional: empty = all SPOT instruments for the account;
  - a single push may contain multiple fills — handler is called once per fill.
*/
func (s *StreamClient) WatchFills(
	ctx context.Context, instID string,
	handler func(types.Fill), errHandler func(error),
) error {
	if !s.c.signerEnabled() {
		var err error = okx.NewError(okx.ErrorKindAuth, "", "stream.WatchFills: credentials required", nil)
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel:  "fills",
		InstType: "SPOT",
		InstID:   instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawFillPush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchFills: parse", okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				if instID != "" && pushes[i].InstID != instID {
					continue
				}
				handler(convertFill(pushes[i]))
			}
		},
	}
	s.c.privateConn().Start(ctx)
	if err := s.c.privateConn().Subscribe(sub); err != nil {
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	return nil
}

// convertFill maps raw push fills to a typed Fill.
func convertFill(p rawFillPush) types.Fill {
	var f types.Fill
	f.InstType = types.InstType(p.InstType)
	f.InstID = p.InstID
	f.TradeID = p.TradeID
	f.OrdID = p.OrdID
	f.ClOrdID = p.ClOrdID
	f.BillID = p.BillID
	f.Tag = p.Tag
	f.FillPx, _ = codec.ParseDecimal(p.FillPx)
	f.FillSz, _ = codec.ParseDecimal(p.FillSz)
	f.FillPxVol, _ = codec.ParseDecimal(p.FillPxVol)
	f.FillPxUsd, _ = codec.ParseDecimal(p.FillPxUsd)
	f.FillMarkVol, _ = codec.ParseDecimal(p.FillMarkVol)
	f.FillFwdPx, _ = codec.ParseDecimal(p.FillFwdPx)
	f.FillMarkPx, _ = codec.ParseDecimal(p.FillMarkPx)
	f.Side = types.SideType(p.Side)
	f.PosSide = p.PosSide
	f.ExecType = p.ExecType
	f.FeeCcy = p.FeeCcy
	f.Fee, _ = codec.ParseDecimal(p.Fee)
	f.FillPnl, _ = codec.ParseDecimal(p.FillPnl)
	f.FillTime, _ = codec.ParseInt64(p.FillTime)
	f.Ts, _ = codec.ParseInt64(p.Ts)
	return f
}
