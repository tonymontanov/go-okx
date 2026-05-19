/*
FILE: swap/stream.go

DESCRIPTION:
Domain sub-client for SWAP WebSocket subscriptions. Implements Watch* methods
on top of internal/ws.Conn. The entire SWAP client uses one public-conn
(for books/bbo-tbt/mark-price/index-tickers/trades) and one private-conn
(for positions/orders).

GENERAL PATTERN FOR EACH WATCH*:
  1. Lazily start the corresponding ws.Conn under ctx.
  2. Register a subscription: the handler takes env.Data (JSON array),
     parses a typed struct, and calls the user callback.
  3. Return nil (or a validation error). The stream lives until ctx is cancelled.

ERROR HANDLING:
  - Local parse errors are NOT critical — log+drop, errHandler is not called
    (calling it for every malformed frame would be noisy).
  - Critical errors (e.g. attempting a private channel without credentials)
    call errHandler synchronously and return an error from Watch*.
  - Reconnect and resubscribe are fully transparent: the user callback is not
    notified on reconnect, but the subscription Reset function is called
    (important for OrderbookEngine).

CANCELLATION:
  - On ctx.Done() the ws.Conn supervise-loop terminates on its own; the SDK
    does NOT call Unsubscribe (a cancelled ctx closes the socket anyway).
*/

package swap

import (
	"context"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/ws"
	"github.com/tonymontanov/go-okx/v2/orderbook"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// StreamClient — WebSocket subscriptions sub-client.
type StreamClient struct {
	c *Client
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{c: c}
}

// ----------------------------------------------------------------------------
// PUBLIC streams
// ----------------------------------------------------------------------------

// rawBookPush — push data from the books channel (one element of data[]).
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
via orderbook.Engine. On each successful application (snapshot or update) the
callback receives a copy of the top-`depth` levels. depth <= 0 ⇒ 25 levels.

Internally:
  - a separate *orderbook.Engine is created for this subscription;
  - in Subscription.Reset() the local engine is reset before reconnect,
    after which the next 'snapshot' push immediately initializes it.

If a gap is detected (Sequence or Checksum), the engine is marked dirty and
errHandler is NOT called — we wait for the next 'snapshot' from OKX (which
arrives automatically on books resubscription after resync). If the gap persists,
ws.Conn may perform a manual re-subscribe every N messages; in M3 the basic
strategy is wait-for-next-snapshot.
*/
func (s *StreamClient) WatchOrderbook(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books", instID, depth, handler, errHandler)
}

/*
WatchOrderbookL2Tbt — full L2 order book tick-by-tick. Channel books-l2-tbt.
Push on every change, without 100ms batching.

OKX REQUIREMENTS: VIP4+ or Market Maker. Otherwise the server will respond 60018.

WHEN TO USE:
  - latency arbitrage, cross-exchange MM, strategies with intervals ≤10ms.

WHEN NOT TO USE:
  - regular MM with 100ms+ intervals — "books" is sufficient.
*/
func (s *StreamClient) WatchOrderbookL2Tbt(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books-l2-tbt", instID, depth, handler, errHandler)
}

/*
WatchOrderbookBooks50L2Tbt — top-50 L2 order book tick-by-tick. Channel
books50-l2-tbt. Intermediate between "books" and "books-l2-tbt" in requirements
and load.

OKX REQUIREMENTS: VIP2+ or Market Maker.
*/
func (s *StreamClient) WatchOrderbookBooks50L2Tbt(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books50-l2-tbt", instID, depth, handler, errHandler)
}

/*
WatchOrderbookBooks5 — top-5 levels, 100ms batch, snapshot-only.
Does not use orderbook.Engine; push arrives as a FULL snapshot
(no incremental updates).

WHEN TO USE:
  - strategies that need only top-5;
  - no client-side state or consistency check required.

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

// parseBookLevels converts [["px","sz","-","ordersCnt"], ...] → []OrderBookLevel.
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

// rawBboPush — push data from the bbo-tbt channel (one-shot best bid/ask).
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

// rawMarkPricePush — push data from the mark-price channel.
type rawMarkPricePush struct {
	InstID   string `json:"instId"`
	MarkPx   string `json:"markPx"`
	Ts       string `json:"ts"`
}

// WatchMarkPrice — mark-price channel.
func (s *StreamClient) WatchMarkPrice(
	ctx context.Context, instID string,
	handler func(price float64, tsMs int64), errHandler func(error),
) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "stream.WatchMarkPrice: InstID is empty", nil)
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel: "mark-price",
		InstID:  instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawMarkPricePush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchMarkPrice: parse", okx.Str("instId", instID), okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				var px float64
				var ts int64
				px, _ = codec.ParseFloat64(pushes[i].MarkPx)
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

// rawIndexTickerPush — push data from the index-tickers channel.
type rawIndexTickerPush struct {
	InstID  string `json:"instId"`
	IdxPx   string `json:"idxPx"`
	Ts      string `json:"ts"`
}

// WatchIndexPrice — index-tickers channel. Accepts the index instId
// ("BTC-USDT", without -SWAP), but the SDK also accepts the swap instId —
// it will be valid: index-tickers subscribes by the index instId
// (see OKX docs). The value is forwarded exactly as provided by the caller.
func (s *StreamClient) WatchIndexPrice(
	ctx context.Context, instID string,
	handler func(price float64, tsMs int64), errHandler func(error),
) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "stream.WatchIndexPrice: InstID is empty", nil)
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel: "index-tickers",
		InstID:  instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawIndexTickerPush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchIndexPrice: parse", okx.Str("instId", instID), okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				var px float64
				var ts int64
				px, _ = codec.ParseFloat64(pushes[i].IdxPx)
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

// rawTradePush — push data from the trades channel.
type rawTradePush struct {
	InstID  string `json:"instId"`
	TradeID string `json:"tradeId"`
	Px      string `json:"px"`
	Sz      string `json:"sz"`
	Side    string `json:"side"`
	Ts      string `json:"ts"`
}

// WatchLastPrice — trades channel, delivers only price and timestamp (for core compatibility).
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

// WatchAggTrades — trades channel. Delivers a full AggTrade (price+size+side+isBuyerMaker+ts).
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
				t.IsBuyerMaker = p.Side == "sell" // taker=sell ⇒ maker=buyer
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

// rawPositionPush — push data from the positions channel.
type rawPositionPush struct {
	InstID  string `json:"instId"`
	PosSide string `json:"posSide"`
	Pos     string `json:"pos"`
	AvgPx   string `json:"avgPx"`
	Upl     string `json:"upl"`
	LiqPx   string `json:"liqPx"`
	UTime   string `json:"uTime"`
}

// WatchPosition — positions channel (private). Filters by instID if provided;
// empty instID ⇒ all SWAP positions.
func (s *StreamClient) WatchPosition(
	ctx context.Context, instID string,
	handler func(types.PositionInfo), errHandler func(error),
) error {
	if !s.c.signerEnabled() {
		var err error = okx.NewError(okx.ErrorKindAuth, "", "stream.WatchPosition: credentials required", nil)
		if errHandler != nil {
			errHandler(err)
		}
		return err
	}
	var sub *ws.Subscription = &ws.Subscription{
		Channel:  "positions",
		InstType: "SWAP",
		InstID:   instID,
		Handler: func(_ string, payload []byte) {
			var pushes []rawPositionPush
			if err := codec.Unmarshal(payload, &pushes); err != nil {
				s.c.logger().Warn("stream.WatchPosition: parse", okx.Err(err))
				return
			}
			var i int
			for i = 0; i < len(pushes); i++ {
				var p rawPositionPush = pushes[i]
				if instID != "" && p.InstID != instID {
					continue
				}
				var info types.PositionInfo
				info.InstID = p.InstID
				info.PosSide = types.PosSide(p.PosSide)
				info.Position, _ = codec.ParseDecimal(p.Pos)
				info.AvgEntryPrice, _ = codec.ParseDecimal(p.AvgPx)
				info.UnrealizedPnL, _ = codec.ParseDecimal(p.Upl)
				info.LiqPrice, _ = codec.ParseDecimal(p.LiqPx)
				info.UpdatedAtMs, _ = codec.ParseInt64(p.UTime)
				handler(info)
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

// rawOrderPush — push data from the orders channel.
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

// WatchAccount — account channel (private). Each push message contains a full
// snapshot of the unified-account balance; the callback receives types.Balance
// with the same set of fields as REST GetBalance. Convenient for the trading
// core: the same domain model is used at startup (REST snapshot) and for live
// updates. Without credentials returns ErrorKindAuth.
//
// The account channel is account-wide — no instType/instId. Subscription lives
// until ctx is cancelled; reconnect/relogin/resubscribe are transparent to the callback.
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

// WatchOpenOrders — orders channel (private). Callback receives the list of
// orders sent in a single push (one or multiple simultaneously).
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
		InstType: "SWAP",
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

// rawFillPush — push data from the fills channel.
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
WatchFills — private "fills" channel filtered by InstType="SWAP". Sends a push
on every fill (full or partial) with the minimum exchange latency — LOWER than
the "orders" channel, where fills are visible as part of the order state machine.

OKX REQUIREMENTS: VIP5+ or Market Maker. Otherwise the server will respond 60018.

ADVANTAGE:
  - lower fill-event latency vs parsing "orders";
  - no noisy intermediate state-updates (live → live amend → ...);
  - contains fillPnl and execType (T/M) ready-to-use — no GetFill round-trip needed.

PARAMETERS:
  - instID is optional: empty = all SWAP instruments for the account;
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
		InstType: "SWAP",
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
