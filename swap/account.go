/*
FILE: swap/account.go

DESCRIPTION:
Domain sub-client for SWAP account/position operations. Implements:
  - GetBalance                       : GET /api/v5/account/balance[?ccy=...]
  - GetPosition / GetSymbolPosition  : GET /api/v5/account/positions?instType=SWAP&instId=...
  - GetOpenOrders                    : GET /api/v5/trade/orders-pending?instType=SWAP&instId=...
  - ClosePosition                    : POST /api/v5/trade/close-position (market close)
  - SetLeverage                      : POST /api/v5/account/set-leverage
  - SetPositionMode                  : POST /api/v5/account/set-position-mode

OKX SPECIFICS:
  - In net-mode (default) an instrument can have at most one position:
    the sign of Position indicates direction (+long/-short). PosSide is always "net".
  - In hedge-mode (long_short_mode) one instrument can have up to two
    positions (long + short). In v1 we return an array; the GetSymbolPosition
    helper with the default (net) mode picks the first entry.
  - ClosePosition allows a market close without needing to know the size.
*/

package swap

import (
	"context"
	"net/url"
	"strings"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/rest"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// AccountClient — account/positions sub-client.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// rawBalanceDetail — raw format of a per-currency entry from /account/balance.
type rawBalanceDetail struct {
	Ccy       string `json:"ccy"`
	Eq        string `json:"eq"`
	CashBal   string `json:"cashBal"`
	AvailEq   string `json:"availEq"`
	AvailBal  string `json:"availBal"`
	FrozenBal string `json:"frozenBal"`
	OrdFrozen string `json:"ordFrozen"`
	Upl       string `json:"upl"`
	IsoUpl    string `json:"isoUpl"`
	DisEq     string `json:"disEq"`
	EqUsd     string `json:"eqUsd"`
	MgnRatio  string `json:"mgnRatio"`
	UTime     string `json:"uTime"`
}

// rawBalance — raw format of the top-level entry from /account/balance.
type rawBalance struct {
	TotalEq     string             `json:"totalEq"`
	AdjEq       string             `json:"adjEq"`
	IsoEq       string             `json:"isoEq"`
	OrdFroz     string             `json:"ordFroz"`
	Imr         string             `json:"imr"`
	Mmr         string             `json:"mmr"`
	MgnRatio    string             `json:"mgnRatio"`
	NotionalUsd string             `json:"notionalUsd"`
	UTime       string             `json:"uTime"`
	Details     []rawBalanceDetail `json:"details"`
}

/*
GetBalance returns the unified-account balance. If currencies are provided,
the OKX response is filtered by them (ccy=BTC,USDT parameter). Without
arguments all currencies with any balance/position are returned.

ENDPOINT: GET /api/v5/account/balance[?ccy=BTC,USDT]

NOTE:
  - The endpoint returns an array of exactly one element (by OKX design).
  - In Demo mode (Config.Demo=true) the balance comes from the sandbox, not
    the real portfolio.
*/
func (a *AccountClient) GetBalance(ctx context.Context, ccy ...string) (types.Balance, error) {
	var q url.Values
	if len(ccy) > 0 {
		q = url.Values{}
		q.Set("ccy", strings.Join(ccy, ","))
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v5/account/balance",
		Query:  q,
		Signed: true,
		Meta: rest.RequestMeta{
			Category: string(okx.RateLimitCategoryQuery),
			// Symbols empty: balance is not per-instrument.
		},
	})
	if err != nil {
		return types.Balance{}, err
	}

	var raws []rawBalance
	if err = resp.UnmarshalData(&raws); err != nil {
		return types.Balance{}, okx.NewError(okx.ErrorKindUnknown, "", "account.GetBalance: parse", err)
	}
	if len(raws) == 0 {
		return types.Balance{}, nil
	}
	return convertBalance(raws[0]), nil
}

func convertBalance(r rawBalance) types.Balance {
	var out types.Balance
	out.TotalEquityUSD, _ = codec.ParseDecimal(r.TotalEq)
	out.AdjustedEquityUSD, _ = codec.ParseDecimal(r.AdjEq)
	out.IsolatedEquityUSD, _ = codec.ParseDecimal(r.IsoEq)
	out.OrderFrozenUSD, _ = codec.ParseDecimal(r.OrdFroz)
	out.InitialMarginUSD, _ = codec.ParseDecimal(r.Imr)
	out.MaintenanceMarginUSD, _ = codec.ParseDecimal(r.Mmr)
	out.MarginRatio, _ = codec.ParseDecimal(r.MgnRatio)
	out.NotionalUSD, _ = codec.ParseDecimal(r.NotionalUsd)
	out.UpdatedAtMs, _ = codec.ParseInt64(r.UTime)

	out.Details = make([]types.BalanceDetail, 0, len(r.Details))
	var i int
	for i = 0; i < len(r.Details); i++ {
		var d rawBalanceDetail = r.Details[i]
		var bd types.BalanceDetail = types.BalanceDetail{Ccy: d.Ccy}
		bd.Equity, _ = codec.ParseDecimal(d.Eq)
		bd.CashBalance, _ = codec.ParseDecimal(d.CashBal)
		bd.AvailableEquity, _ = codec.ParseDecimal(d.AvailEq)
		bd.AvailableBalance, _ = codec.ParseDecimal(d.AvailBal)
		bd.FrozenBalance, _ = codec.ParseDecimal(d.FrozenBal)
		bd.OrderFrozen, _ = codec.ParseDecimal(d.OrdFrozen)
		bd.UnrealizedPnL, _ = codec.ParseDecimal(d.Upl)
		bd.IsolatedUnrealizedPnL, _ = codec.ParseDecimal(d.IsoUpl)
		bd.DiscountEquity, _ = codec.ParseDecimal(d.DisEq)
		bd.EquityUSD, _ = codec.ParseDecimal(d.EqUsd)
		bd.MarginRatio, _ = codec.ParseDecimal(d.MgnRatio)
		bd.UpdatedAtMs, _ = codec.ParseInt64(d.UTime)
		out.Details = append(out.Details, bd)
	}
	return out
}

// rawPositionEntry — raw response from /account/positions (used fields only).
type rawPositionEntry struct {
	InstID  string `json:"instId"`
	PosSide string `json:"posSide"`
	Pos     string `json:"pos"`
	AvgPx   string `json:"avgPx"`
	Upl     string `json:"upl"`
	LiqPx   string `json:"liqPx"`
	UTime   string `json:"uTime"`
}

/*
GetPositions returns all open positions for a SWAP instrument (1 in net-mode,
up to 2 in hedge-mode). If there is no position — returns an empty slice, not an error.
*/
func (a *AccountClient) GetPositions(ctx context.Context, instID string) ([]types.PositionInfo, error) {
	if instID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "account.GetPositions: InstID is empty", nil)
	}

	var q url.Values = url.Values{}
	q.Set("instType", string(types.InstTypeSWAP))
	q.Set("instId", instID)

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v5/account/positions",
		Query:  q,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{instID},
			Category: string(okx.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return nil, err
	}

	var raws []rawPositionEntry
	if err = resp.UnmarshalData(&raws); err != nil {
		return nil, okx.NewError(okx.ErrorKindUnknown, "", "account.GetPositions: parse", err)
	}

	var out []types.PositionInfo = make([]types.PositionInfo, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		var r rawPositionEntry = raws[i]
		var info types.PositionInfo = types.PositionInfo{InstID: r.InstID, PosSide: types.PosSide(r.PosSide)}
		info.Position, err = codec.ParseDecimal(r.Pos)
		if err != nil {
			a.c.logger().Warn("account.GetPositions: parse pos", okx.Str("instId", r.InstID), okx.Err(err))
		}
		info.AvgEntryPrice, _ = codec.ParseDecimal(r.AvgPx)
		info.UnrealizedPnL, _ = codec.ParseDecimal(r.Upl)
		info.LiqPrice, _ = codec.ParseDecimal(r.LiqPx)
		info.UpdatedAtMs, _ = codec.ParseInt64(r.UTime)
		out = append(out, info)
	}
	return out, nil
}

/*
GetSymbolPosition returns the position for a single instrument in net-mode (or
the first one if multiple arrived for some reason). In hedge-mode use
GetPositions and filter by PosSide yourself.
*/
func (a *AccountClient) GetSymbolPosition(ctx context.Context, instID string) (types.PositionInfo, error) {
	var positions []types.PositionInfo
	var err error
	positions, err = a.GetPositions(ctx, instID)
	if err != nil {
		return types.PositionInfo{}, err
	}
	if len(positions) == 0 {
		return types.PositionInfo{InstID: instID, PosSide: types.PosSideNet}, nil
	}
	return positions[0], nil
}

// rawOpenOrderEntry — raw response from /trade/orders-pending. Fields match
// /trade/order-history (the same struct is used there too).
type rawOpenOrderEntry struct {
	InstID    string `json:"instId"`
	OrdID     string `json:"ordId"`
	ClOrdID   string `json:"clOrdId"`
	OrdType   string `json:"ordType"`
	Side      string `json:"side"`
	Px        string `json:"px"`
	Sz        string `json:"sz"`
	FillSz    string `json:"fillSz"`
	AccFillSz string `json:"accFillSz"`
	State     string `json:"state"`
	CTime     string `json:"cTime"`
	UTime     string `json:"uTime"`
}

/*
GetOpenOrders returns all active orders for an instrument. OKX returns up to
100 per call; the SDK does NOT paginate — in a real HFT scenario 100 open
orders on one instrument is already a lot; if more is needed, iteration by
beforeID can be added later.
*/
func (a *AccountClient) GetOpenOrders(ctx context.Context, instID string) ([]types.OrderInfo, error) {
	if instID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "account.GetOpenOrders: InstID is empty", nil)
	}

	var q url.Values = url.Values{}
	q.Set("instType", string(types.InstTypeSWAP))
	q.Set("instId", instID)

	var resp rest.Response
	var rateLimits map[string]string
	var err error
	resp, rateLimits, err = a.c.rest().Do(ctx, rest.Options{
		Method: "GET",
		Path:   "/api/v5/trade/orders-pending",
		Query:  q,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{instID},
			Category: string(okx.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return nil, err
	}

	var raws []rawOpenOrderEntry
	if err = resp.UnmarshalData(&raws); err != nil {
		return nil, okx.NewError(okx.ErrorKindUnknown, "", "account.GetOpenOrders: parse", err)
	}

	var out []types.OrderInfo = make([]types.OrderInfo, 0, len(raws))
	var i int
	for i = 0; i < len(raws); i++ {
		var r rawOpenOrderEntry = raws[i]
		var info types.OrderInfo = types.OrderInfo{
			OrderID:       r.OrdID,
			ClientOrderID: r.ClOrdID,
			InstID:        r.InstID,
			Side:          types.SideType(r.Side),
			OrderType:     types.OrderType(r.OrdType),
			State:         types.ParseOrderState(r.State),
			RateLimits:    rateLimits,
		}
		info.Price, _ = codec.ParseDecimal(r.Px)
		info.Size, _ = codec.ParseDecimal(r.Sz)
		if r.AccFillSz != "" {
			info.FilledSize, _ = codec.ParseDecimal(r.AccFillSz)
		} else {
			info.FilledSize, _ = codec.ParseDecimal(r.FillSz)
		}
		info.CreatedAtMs, _ = codec.ParseInt64(r.CTime)
		info.UpdatedAtMs, _ = codec.ParseInt64(r.UTime)
		out = append(out, info)
	}
	return out, nil
}

/*
ClosePosition closes a position at market. Uses
POST /api/v5/trade/close-position. mgnMode defaults to cross.

If there is no position — OKX returns error 51400/51169; returned as-is,
the caller decides whether this is a "normal" situation.
*/
func (a *AccountClient) ClosePosition(ctx context.Context, instID string) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "account.ClosePosition: InstID is empty", nil)
	}

	var body map[string]any = map[string]any{
		"instId":  instID,
		"mgnMode": string(types.TdModeCross),
	}

	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/trade/close-position",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			// close-position sends a market-order internally — counted as
			// Place (charged against sub-account 1000/2s budget and
			// per-symbol budget). OrderCount=1 because one market-close.
			OrderCount: 1,
			Symbols:    []string{instID},
			Category:   string(okx.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return err
	}
	_ = resp
	return nil
}

/*
SetLeverage sets the leverage for an instrument and mgnMode. Default for
SWAP is cross. In hedge-mode (long_short_mode) posSide is additionally required;
the SDK sets leverage for both sides here.
*/
func (a *AccountClient) SetLeverage(ctx context.Context, instID string, leverage int64) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "account.SetLeverage: InstID is empty", nil)
	}
	if leverage <= 0 {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "account.SetLeverage: leverage must be positive", nil)
	}

	var body map[string]any = map[string]any{
		"instId":  instID,
		"lever":   leverage,
		"mgnMode": string(types.TdModeCross),
	}
	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/account/set-leverage",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  []string{instID},
			Category: string(okx.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return err
	}
	_ = resp
	return nil
}

/*
SetPositionMode sets the account position mode (net_mode / long_short_mode).
Applies to the entire account. The SDK defaults to net_mode.
*/
func (a *AccountClient) SetPositionMode(ctx context.Context, oneWay bool) error {
	var mode types.PositionMode = types.PositionModeNet
	if !oneWay {
		mode = types.PositionModeLongShort
	}
	var body map[string]any = map[string]any{"posMode": string(mode)}
	var resp rest.Response
	var err error
	resp, _, err = a.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/account/set-position-mode",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			// set-position-mode is account-wide, instId is not passed.
			Category: string(okx.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return err
	}
	_ = resp
	return nil
}
