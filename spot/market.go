/*
ФАЙЛ: spot/market.go

ОПИСАНИЕ:
Доменный саб-клиент рыночных данных SPOT:
  - GetSymbolInfo         : GET /api/v5/public/instruments?instType=SPOT&instId=...
  - GetOrderBook          : GET /api/v5/market/books?instId=...&sz=...
  - GetHistoricalCandles  : GET /api/v5/market/history-candles?instId=...&bar=...

ENDPOINT'Ы /market/books, /market/history-candles ОДИНАКОВЫ для SPOT и SWAP,
определение SPOT — только в /public/instruments через query instType=SPOT
(и в формате instID без суффикса "-SWAP").

ОТЛИЧИЯ ОТ SWAP /public/instruments?instType=SWAP:
  - НЕТ полей ctVal/ctMult/settleCcy в ответе для SPOT.
  - НЕТ maxMktSz для market BUY с tgtCcy=quote_ccy (но maxMktSz приходит в base
    для tgtCcy=base_ccy / sell). Парсим как есть.

SUB-MINUTE TIMEFRAMES — те же ограничения, что в SWAP (REST не поддерживает 1s/15s/30s).
*/

package spot

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/rest"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

// MarketDataClient — саб-клиент рыночных данных SPOT.
type MarketDataClient struct {
	c *Client
}

func newMarketDataClient(c *Client) *MarketDataClient {
	return &MarketDataClient{c: c}
}

// rawInstrumentEntry — сырой ответ /public/instruments для SPOT.
// CtVal/CtMult/SettleCcy для SPOT отсутствуют (или пустые) — оставляем поля,
// чтобы переиспользовать общую структуру; при парсинге попадут в пустые
// decimal/string без ошибок.
type rawInstrumentEntry struct {
	InstID   string `json:"instId"`
	BaseCcy  string `json:"baseCcy"`
	QuoteCcy string `json:"quoteCcy"`
	TickSz   string `json:"tickSz"`
	LotSz    string `json:"lotSz"`
	MinSz    string `json:"minSz"`
	MaxLmtSz string `json:"maxLmtSz"`
	MaxMktSz string `json:"maxMktSz"`
}

/*
GetSymbolInfo возвращает спецификацию SPOT-инструмента.
*/
func (m *MarketDataClient) GetSymbolInfo(ctx context.Context, instID string) (types.SymbolInfo, error) {
	var info types.SymbolInfo
	if instID == "" {
		return info, okx.NewError(okx.ErrorKindInvalidRequest, "", "market.GetSymbolInfo: InstID is empty", nil)
	}

	var q url.Values = url.Values{}
	q.Set("instType", string(types.InstTypeSpot))
	q.Set("instId", instID)

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v5/public/instruments",
		Query:  q,
		Signed: false,
		Meta: rest.RequestMeta{
			Symbols:  []string{instID},
			Category: string(okx.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return info, err
	}

	var raws []rawInstrumentEntry
	if err = resp.UnmarshalData(&raws); err != nil {
		return info, okx.NewError(okx.ErrorKindUnknown, "", "market.GetSymbolInfo: parse", err)
	}
	if len(raws) == 0 {
		return info, okx.NewError(okx.ErrorKindInvalidRequest, "", "market.GetSymbolInfo: instrument not found", nil)
	}

	var r rawInstrumentEntry = raws[0]
	info.InstID = r.InstID
	info.BaseCcy = r.BaseCcy
	info.QuoteCcy = r.QuoteCcy
	info.TickSize, _ = codec.ParseDecimal(r.TickSz)
	info.LotSize, _ = codec.ParseDecimal(r.LotSz)
	info.MinSize, _ = codec.ParseDecimal(r.MinSz)
	info.MaxLimitSize, _ = codec.ParseDecimal(r.MaxLmtSz)
	info.MaxMarketSize, _ = codec.ParseDecimal(r.MaxMktSz)
	info.PricePrecision = int32(decimalScale(r.TickSz))
	info.QuantityPrecision = int32(decimalScale(r.LotSz))

	return info, nil
}

// decimalScale возвращает количество значащих знаков после точки в строке
// "0.0001" → 4. Дублирует логику swap/market.go (5 строк, не имеет смысла
// тащить через внутренний пакет).
func decimalScale(s string) int {
	var dotIdx int = -1
	var i int
	for i = 0; i < len(s); i++ {
		if s[i] == '.' {
			dotIdx = i
			break
		}
	}
	if dotIdx < 0 {
		return 0
	}
	var scale int = len(s) - dotIdx - 1
	for scale > 0 && s[dotIdx+scale] == '0' {
		scale--
	}
	return scale
}

// rawOrderBookResponse — формат идентичен SWAP.
type rawOrderBookResponse struct {
	Asks  [][]string `json:"asks"`
	Bids  [][]string `json:"bids"`
	Ts    string     `json:"ts"`
	SeqID *int64     `json:"seqId,omitempty"`
}

/*
GetOrderBook возвращает снапшот стакана SPOT. depth ∈ {1, 5, 10, 20, 50, 100, 400}.
*/
func (m *MarketDataClient) GetOrderBook(ctx context.Context, instID string, depth int) (types.OrderBookSnapshot, error) {
	var snap types.OrderBookSnapshot
	if instID == "" {
		return snap, okx.NewError(okx.ErrorKindInvalidRequest, "", "market.GetOrderBook: InstID is empty", nil)
	}
	if depth <= 0 {
		depth = 20
	}

	var q url.Values = url.Values{}
	q.Set("instId", instID)
	q.Set("sz", strconv.Itoa(depth))

	var resp rest.Response
	var err error
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v5/market/books",
		Query:  q,
		Signed: false,
		Meta: rest.RequestMeta{
			Symbols:  []string{instID},
			Category: string(okx.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return snap, err
	}

	var raws []rawOrderBookResponse
	if err = resp.UnmarshalData(&raws); err != nil {
		return snap, okx.NewError(okx.ErrorKindUnknown, "", "market.GetOrderBook: parse", err)
	}
	if len(raws) == 0 {
		return snap, okx.NewError(okx.ErrorKindExchange, "", "market.GetOrderBook: empty data", nil)
	}

	var r rawOrderBookResponse = raws[0]
	snap.InstID = instID
	snap.Bids = parseLevels(r.Bids)
	snap.Asks = parseLevels(r.Asks)
	snap.Ts, _ = codec.ParseInt64(r.Ts)
	if r.SeqID != nil {
		snap.SeqID = *r.SeqID
	}
	return snap, nil
}

func parseLevels(raw [][]string) []types.OrderBookLevel {
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

/*
GetHistoricalCandles — исторические свечи. length ∈ [1..300]. Sub-minute
таймфреймы (1s/15s/30s) приведут к ErrorKindInvalidRequest без обращения к
бирже (как на SWAP).
*/
func (m *MarketDataClient) GetHistoricalCandles(
	ctx context.Context, instID string, tf types.Timeframe, length int,
) (types.Candles, error) {
	if instID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "market.GetHistoricalCandles: InstID is empty", nil)
	}
	if length <= 0 {
		length = 100
	}

	var bar string
	var err error
	bar, err = timeframeToOKX(tf)
	if err != nil {
		return nil, err
	}

	var q url.Values = url.Values{}
	q.Set("instId", instID)
	q.Set("bar", bar)
	q.Set("limit", strconv.Itoa(length))

	var resp rest.Response
	resp, _, err = m.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v5/market/history-candles",
		Query:  q,
		Signed: false,
		Meta: rest.RequestMeta{
			Symbols:  []string{instID},
			Category: string(okx.RateLimitCategoryMarketData),
		},
	})
	if err != nil {
		return nil, err
	}

	var raws [][]string
	if err = resp.UnmarshalData(&raws); err != nil {
		return nil, okx.NewError(okx.ErrorKindUnknown, "", "market.GetHistoricalCandles: parse", err)
	}

	var candles types.Candles = make(types.Candles, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		var row []string = raws[i]
		if len(row) < 7 {
			continue
		}
		var candle types.Candle
		candle.OpenTimeMs, _ = codec.ParseInt64(row[0])
		candle.Open, _ = codec.ParseDecimal(row[1])
		candle.High, _ = codec.ParseDecimal(row[2])
		candle.Low, _ = codec.ParseDecimal(row[3])
		candle.Close, _ = codec.ParseDecimal(row[4])
		candle.Volume, _ = codec.ParseDecimal(row[5])
		candle.VolumeQuote, _ = codec.ParseDecimal(row[6])
		if len(row) >= 9 {
			candle.Closed = row[8] == "1"
		}
		candles = append(candles, candle)
	}
	return candles, nil
}

func timeframeToOKX(tf types.Timeframe) (string, error) {
	switch tf {
	case types.Timeframe1s, types.Timeframe15s, types.Timeframe30s:
		return "", okx.NewError(
			okx.ErrorKindInvalidRequest, "",
			fmt.Sprintf("market.GetHistoricalCandles: sub-minute timeframe %q is not supported by REST", tf),
			nil,
		)
	case types.Timeframe1m:
		return "1m", nil
	case types.Timeframe3m:
		return "3m", nil
	case types.Timeframe5m:
		return "5m", nil
	case types.Timeframe15m:
		return "15m", nil
	case types.Timeframe30m:
		return "30m", nil
	case types.Timeframe1h:
		return "1H", nil
	case types.Timeframe2h:
		return "2H", nil
	case types.Timeframe4h:
		return "4H", nil
	case types.Timeframe6h:
		return "6H", nil
	case types.Timeframe8h:
		return "8H", nil
	case types.Timeframe12h:
		return "12H", nil
	case types.Timeframe1D:
		return "1D", nil
	case types.Timeframe1W:
		return "1W", nil
	case types.Timeframe1Mo:
		return "1M", nil
	default:
		return "", okx.NewError(okx.ErrorKindInvalidRequest, "", fmt.Sprintf("market.GetHistoricalCandles: unsupported timeframe %q", tf), nil)
	}
}
