/*
FILE: swap/trading_fills.go

DESCRIPTION:
REST methods for retrieving fills for the SWAP profile. Structurally mirrors
spot/trading_fills.go: same query parameters, same rawFillPush,
same convertFill. The difference is the instType="SWAP" filter.
*/

package swap

import (
	"context"
	"fmt"
	"net/url"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/rest"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// GetFills — last 3 days of SWAP fills.
func (t *TradingClient) GetFills(ctx context.Context, q types.FillsQuery) ([]types.Fill, error) {
	return t.fetchFills(ctx, "/api/v5/trade/fills", q)
}

// GetFillsHistory — up to 3 months of SWAP fills.
func (t *TradingClient) GetFillsHistory(ctx context.Context, q types.FillsQuery) ([]types.Fill, error) {
	return t.fetchFills(ctx, "/api/v5/trade/fills-history", q)
}

/*
GetFill returns all fills for a specific order (optional filter by tradeID).
Implementation — GetFills filtered by ordID + post-filter by tradeID.
*/
func (t *TradingClient) GetFill(ctx context.Context, instID, ordID, tradeID string) ([]types.Fill, error) {
	if ordID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.GetFill: OrderID is empty", nil)
	}
	var fills []types.Fill
	var err error
	fills, err = t.GetFills(ctx, types.FillsQuery{
		InstID: instID,
		OrdID:  ordID,
		Limit:  100,
	})
	if err != nil {
		return nil, err
	}
	if tradeID == "" {
		return fills, nil
	}
	var out []types.Fill = make([]types.Fill, 0, 1)
	var i int
	for i = 0; i < len(fills); i++ {
		if fills[i].TradeID == tradeID {
			out = append(out, fills[i])
		}
	}
	return out, nil
}

func (t *TradingClient) fetchFills(ctx context.Context, path string, q types.FillsQuery) ([]types.Fill, error) {
	var query url.Values = make(url.Values, 8)
	query.Set("instType", "SWAP")
	if q.InstID != "" {
		query.Set("instId", q.InstID)
	}
	if q.OrdID != "" {
		query.Set("ordId", q.OrdID)
	}
	if q.After != "" {
		query.Set("after", q.After)
	}
	if q.Before != "" {
		query.Set("before", q.Before)
	}
	if q.BeginMs > 0 {
		query.Set("begin", fmt.Sprintf("%d", q.BeginMs))
	}
	if q.EndMs > 0 {
		query.Set("end", fmt.Sprintf("%d", q.EndMs))
	}
	if q.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", q.Limit))
	}

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   path,
		Query:  query,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols: nilOrSlice(q.InstID),
		},
	})
	if err != nil {
		return nil, err
	}
	var rawFills []rawFillPush
	if err = resp.UnmarshalData(&rawFills); err != nil {
		return nil, okx.NewError(okx.ErrorKindUnknown, "", "trading.fetchFills: parse", err)
	}
	var out []types.Fill = make([]types.Fill, 0, len(rawFills))
	var i int
	for i = 0; i < len(rawFills); i++ {
		out = append(out, convertFill(rawFills[i]))
	}
	return out, nil
}

func nilOrSlice(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}
