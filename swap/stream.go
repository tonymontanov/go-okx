/*
ФАЙЛ: swap/stream.go

ОПИСАНИЕ:
Доменный саб-клиент WebSocket-подписок SWAP. Реализация Watch*-методов
поверх internal/ws.Conn. На весь SWAP-клиент используется один public-conn
(для books/bbo-tbt/mark-price/index-tickers/trades) и один private-conn
(для positions/orders). Это согласовано в чате с пользователем.

ОБЩАЯ СХЕМА КАЖДОГО WATCH*:
  1. Лениво стартуем соответствующий ws.Conn под ctx.
  2. Регистрируем подписку: handler берёт env.Data (json массив),
     парсит typed-struct и вызывает пользовательский callback.
  3. Возвращаем nil (или ошибку валидации). Стрим живёт, пока ctx не отменён.

ОБРАБОТКА ОШИБОК:
  - Локальные ошибки парсинга — НЕ критичные, log+drop, errHandler не вызывается
    (вызов на каждый битый кадр будет шумом).
  - Критичные ошибки (например, попытка private-канала без credentials)
    вызывают errHandler синхронно и возвращают ошибку из Watch*.
  - Reconnect и resubscribe полностью прозрачны: пользовательский callback
    при reconnect не уведомляется, но Reset-функция подписки вызывается
    (важно для OrderbookEngine).

ОТМЕНА:
  - При ctx.Done() supervise-loop ws.Conn завершится сам; SDK при этом НЕ
    делает Unsubscribe (canceled ctx → сокет всё равно закрывается).
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

// StreamClient — саб-клиент WebSocket-подписок.
type StreamClient struct {
	c *Client
}

func newStreamClient(c *Client) *StreamClient {
	return &StreamClient{c: c}
}

// ----------------------------------------------------------------------------
// PUBLIC streams
// ----------------------------------------------------------------------------

// rawBookPush — push-данные канала books (один элемент data[]).
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
через orderbook.Engine. На каждое успешное применение (snapshot или update)
callback получает копию топ-`depth` уровней. depth <= 0 ⇒ 25 уровней.

Внутри:
  - создаётся отдельный *orderbook.Engine на эту подписку;
  - в Subscription.Reset() локальный движок сбрасывается перед reconnect,
    после чего следующий 'snapshot' push сразу его инициализирует.

Если детектится gap (Sequence или Checksum), engine помечается dirty и
errHandler НЕ вызывается — мы дожидаемся следующего 'snapshot' от OKX (он
приходит автоматически при подписке на books после resync). Если gap
повторяется устойчиво, ws.Conn раз в N сообщений может сделать ручную
re-subscribe; в M3 базовая стратегия — wait-for-next-snapshot.
*/
func (s *StreamClient) WatchOrderbook(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books", instID, depth, handler, errHandler)
}

/*
WatchOrderbookL2Tbt — full L2-стакан tick-by-tick. Канал books-l2-tbt.
Push на каждое изменение, без 100ms-батчинга.

ТРЕБОВАНИЯ OKX: VIP4+ или Market Maker. Иначе сервер ответит 60018.

КОГДА БРАТЬ:
  - latency-арбитраж, cross-exchange MM, стратегии с интервалами ≤10мс.

КОГДА НЕ БРАТЬ:
  - обычной MM с интервалами 100мс+ достаточно "books".
*/
func (s *StreamClient) WatchOrderbookL2Tbt(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books-l2-tbt", instID, depth, handler, errHandler)
}

/*
WatchOrderbookBooks50L2Tbt — top-50 L2-стакан tick-by-tick. Канал
books50-l2-tbt. Промежуточный между "books" и "books-l2-tbt" по
требованиям и нагрузке.

ТРЕБОВАНИЯ OKX: VIP2+ или Market Maker.
*/
func (s *StreamClient) WatchOrderbookBooks50L2Tbt(
	ctx context.Context, instID string, depth int,
	handler func(types.OrderBookSnapshot), errHandler func(error),
) error {
	return s.watchBookEngine(ctx, "books50-l2-tbt", instID, depth, handler, errHandler)
}

/*
WatchOrderbookBooks5 — top-5 уровней, batch 100ms, snapshot-only.
Не использует orderbook.Engine, push приходит как ПОЛНЫЙ snapshot
(без incremental updates).

КОГДА БРАТЬ:
  - стратегии, которым достаточно top-5;
  - не нужен state на клиенте, не нужна проверка консистентности.

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

// parseBookLevels превращает [["px","sz","-","ordersCnt"], ...] → []OrderBookLevel.
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

// rawBboPush — push-данные канала bbo-tbt (one-shot best bid/ask).
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

// rawMarkPricePush — push-данные канала mark-price.
type rawMarkPricePush struct {
	InstID   string `json:"instId"`
	MarkPx   string `json:"markPx"`
	Ts       string `json:"ts"`
}

// WatchMarkPrice — канал mark-price.
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

// rawIndexTickerPush — push-данные канала index-tickers.
type rawIndexTickerPush struct {
	InstID  string `json:"instId"`
	IdxPx   string `json:"idxPx"`
	Ts      string `json:"ts"`
}

// WatchIndexPrice — канал index-tickers. Принимает индексный instId
// ("BTC-USDT", без -SWAP), но SDK позволяет передать сам swap-instId и
// внутри это будет также допустимо: index-tickers подписан по instId
// именно индекса (см. OKX docs). Здесь мы передаём ровно то, что подал
// пользователь.
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

// rawTradePush — push-данные канала trades.
type rawTradePush struct {
	InstID  string `json:"instId"`
	TradeID string `json:"tradeId"`
	Px      string `json:"px"`
	Sz      string `json:"sz"`
	Side    string `json:"side"`
	Ts      string `json:"ts"`
}

// WatchLastPrice — канал trades, отдаёт только цену и timestamp (для совместимости с core).
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

// WatchAggTrades — канал trades. Отдаёт полную AggTrade (price+size+side+isBuyerMaker+ts).
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

// rawPositionPush — push-данные канала positions.
type rawPositionPush struct {
	InstID  string `json:"instId"`
	PosSide string `json:"posSide"`
	Pos     string `json:"pos"`
	AvgPx   string `json:"avgPx"`
	Upl     string `json:"upl"`
	LiqPx   string `json:"liqPx"`
	UTime   string `json:"uTime"`
}

// WatchPosition — канал positions (приватный). Фильтрует по instID если он
// задан; пустой instID ⇒ все позиции SWAP.
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

// WatchAccount — канал account (приватный). Каждое push-сообщение содержит
// полный снимок баланса unified-account; callback получает types.Balance с
// тем же набором полей, что и REST GetBalance. Это удобно для торгового ядра:
// одна и та же доменная модель используется и на старте (REST snapshot), и
// далее (живые обновления). Без credentials возвращает ErrorKindAuth.
//
// Канал account аккаунтный — без instType/instId. Подписка живёт пока ctx
// не отменён; reconnect/relogin/resubscribe прозрачны для callback'а.
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

// WatchOpenOrders — канал orders (приватный). Callback получает список ордеров,
// присланных в одном push'е (один или несколько одновременно).
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
WatchFills — приватный канал "fills" с фильтром InstType="SWAP". Шлёт
push на каждое исполнение (полное или частичное) с минимальной
для биржи задержкой — НИЖЕ, чем у канала "orders", где исполнения
видны как часть state-machine ордера.

ТРЕБОВАНИЯ OKX: VIP5+ или Market Maker. Иначе сервер ответит 60018.

ВЫИГРЫШ:
  - меньше латентность fill-event'а vs парсинга "orders";
  - нет шумных промежуточных state-update'ов (live → live amend → ...);
  - содержит fillPnl и execType (T/M) в готовом виде — не нужно
    выводить через GetFill round-trip.

ПАРАМЕТРЫ:
  - instID опционален: пустой = все SWAP-инструменты по аккаунту;
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
