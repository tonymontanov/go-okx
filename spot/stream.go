/*
ФАЙЛ: spot/stream.go

ОПИСАНИЕ:
Доменный саб-клиент WebSocket-подписок SPOT. Поверх internal/ws.Conn
с теми же паттернами, что в swap/stream.go:
  - один public-conn на весь spot-client (books, bbo-tbt, trades);
  - один private-conn (orders, account);
  - reconnect/relogin/resubscribe прозрачны для пользователя;
  - локальные ошибки парсинга — log+drop.

КАНАЛЫ OKX для SPOT (то, что мы поддерживаем):
  - "books"      : полный стакан (тот же протокол с checksum/seqId, что у SWAP).
  - "bbo-tbt"    : best bid/ask, tick-by-tick.
  - "trades"     : сделки.
  - "orders"     : приватный, InstType="SPOT" для фильтрации именно спот-ордеров.
  - "account"    : приватный, unified-account balance updates (общий с SWAP).

ЧЕГО ЗДЕСЬ НЕТ (по сравнению с swap/stream.go):
  - mark-price, index-tickers, positions — этих каналов на SPOT нет/не нужны.
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

// StreamClient — саб-клиент WS-подписок SPOT.
type StreamClient struct {
	c *Client
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{c: c}
}

// ----------------------------------------------------------------------------
// PUBLIC streams
// ----------------------------------------------------------------------------

// rawBookPush — push-данные канала books.
type rawBookPush struct {
	Asks      [][]string `json:"asks"`
	Bids      [][]string `json:"bids"`
	Ts        string     `json:"ts"`
	Checksum  int32      `json:"checksum"`
	SeqID     int64      `json:"seqId"`
	PrevSeqID int64      `json:"prevSeqId"`
}

/*
WatchOrderbook подписывается на канал books и поддерживает локальный стакан
через orderbook.Engine. Семантика идентична swap.WatchOrderbook — формат
канала books одинаков для SPOT и SWAP в OKX v5.
*/
func (s *StreamClient) WatchOrderbook(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books", instID, depth, handler, errHandler)
}

/*
WatchOrderbookL2Tbt подписывается на канал books-l2-tbt: ПОЛНЫЙ L2-стакан
tick-by-tick (push на каждое изменение, без батчинга по 100ms). Формат
сообщений идентичен "books" (snapshot+update+checksum+seqId), поэтому
используется тот же orderbook.Engine.

ТРЕБОВАНИЯ OKX: канал доступен только VIP4+ или Market Maker. Если
аккаунт ниже — биржа ответит error-event с code 60018.

КОГДА БРАТЬ:
  - стратегии с очень короткими интервалами котирования (≤10 мс),
    которым важно ловить промежуточные изменения top-of-book;
  - latency-арбитраж и cross-exchange MM.

КОГДА НЕ БРАТЬ:
  - канал гораздо «громче» (по объёму трафика и CPU на парсинг). Для
    обычной MM-стратегии достаточно "books" с 100ms батчингом.
*/
func (s *StreamClient) WatchOrderbookL2Tbt(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books-l2-tbt", instID, depth, handler, errHandler)
}

/*
WatchOrderbookBooks50L2Tbt подписывается на канал books50-l2-tbt: top-50
L2-стакан tick-by-tick. Промежуточный по требованиям и нагрузке между
"books" и "books-l2-tbt".

ТРЕБОВАНИЯ OKX: VIP2+ или Market Maker.
*/
func (s *StreamClient) WatchOrderbookBooks50L2Tbt(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books50-l2-tbt", instID, depth, handler, errHandler)
}

/*
WatchOrderbookBooks5 подписывается на канал books5: top-5 уровней,
batch 100ms. В отличие от других book-каналов, push приходит как
ПОЛНЫЙ snapshot (без incremental updates и checksum'ов). Поэтому
orderbook.Engine не нужен — handler получает snapshot напрямую.

КОГДА БРАТЬ:
  - стратегия использует только top-of-book (top-5 хватает с запасом);
  - не нужна гарантия консистентности с биржей (snapshot самодостаточен);
  - хочется минимальной нагрузки на парсинг и нулевого state на клиенте.

ОГРАНИЧЕНИЯ:
  - depth-параметр игнорируется (всегда 5);
  - SeqID/Checksum в snapshot не заполняются.
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

// watchBookEngine — общая реализация для всех L2-каналов с инкрементальными
// обновлениями (books, books-l2-tbt, books50-l2-tbt). Один формат
// сообщений, один engine, одна логика — отличается только имя канала.
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

// parseBookLevels превращает [["px","sz",...], ...] → []OrderBookLevel.
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

// rawBboPush — push-данные канала bbo-tbt.
type rawBboPush struct {
	Asks [][]string `json:"asks"`
	Bids [][]string `json:"bids"`
	Ts   string     `json:"ts"`
}

/*
WatchSpread подписывается на канал bbo-tbt и отдаёт обновления best bid/ask.
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

// rawTradePush — push-данные канала trades.
type rawTradePush struct {
	InstID  string `json:"instId"`
	TradeID string `json:"tradeId"`
	Px      string `json:"px"`
	Sz      string `json:"sz"`
	Side    string `json:"side"`
	Ts      string `json:"ts"`
}

// WatchLastPrice — канал trades, отдаёт только цену и timestamp.
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

// WatchAggTrades — канал trades, полная AggTrade.
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

// rawOrderPush — push-данные канала orders.
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
WatchAccount — канал account (приватный). Каждое push-сообщение содержит
снимок баланса unified-account. Подписка живёт пока ctx не отменён.

Канал общий для SPOT и SWAP (unified-account), но иметь его и в spot-стриме
важно для приложений, которые работают только со SPOT и не хотят инициализировать
SWAP-клиент (это сэкономит одно WS-соединение).
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
WatchOpenOrders — канал orders (приватный) с фильтром InstType="SPOT".
Callback получает список ордеров, присланных в одном push'е.
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

// rawFillPush — push-данные канала fills.
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
WatchFills — приватный канал "fills" с фильтром InstType="SPOT". Шлёт
push на каждое исполнение (полное или частичное) с минимальной
для биржи задержкой — НИЖЕ, чем у канала "orders", где исполнения
видны как часть state-machine ордера.

ТРЕБОВАНИЯ OKX: VIP5+ или Market Maker. Иначе сервер ответит 60018.

ВЫИГРЫШ:
  - меньше латентность fill-event'а vs парсинга "orders";
  - нет шумных промежуточных state-update'ов (live → live amend → ...);
  - содержит fillPnl и execType (T/M) в готовом виде — не нужно
    выводить через дополнительный round-trip GetFill.

ПАРАМЕТРЫ:
  - instID опционален: пустой = все SPOT-инструменты по аккаунту;
  - в одном push'е может быть несколько fill'ов — handler вызывается
    по одному разу на каждый.
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

// convertFill маппит raw push fills в типизированный Fill.
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
