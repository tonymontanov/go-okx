/*
FILE: spot/account.go

DESCRIPTION:
Domain sub-client for SPOT profile account/balance operations.

Implemented methods:
  - GetBalance      : GET /api/v5/account/balance[?ccy=...]
  - GetOpenOrders   : GET /api/v5/trade/orders-pending?instType=SPOT&instId=...

WHAT IS NOT HERE AND WHY:
  - GetPositions / GetSymbolPosition: on spot OKX does NOT return positions
    via /account/positions. The "what do I hold" state on cash spot is simply
    Balance.Details. Therefore these methods are absent from spot/account.go;
    the spot connector in the trading core builds types.PositionInfo from the
    free-balance of the base currency (see core/internal/connectors/okx/spot).
  - SetLeverage / SetPositionMode: not applicable to cash spot. If spot margin
    (cross/isolated) is added in the future, the methods will go into a separate
    spot/margin/* sub-package to keep the cash-only API uncluttered.
  - ClosePosition: the concept of "a position to close" does not exist on cash spot.

BALANCE ENDPOINT IS SHARED BETWEEN SPOT AND SWAP:
OKX uses unified-account — the balance is not split by instType. Therefore
parsing is identical to swap/account.go and the Balance type is shared
(see spot/types/aliases.go → Balance = swaptypes.Balance).
*/

package spot

import (
	"context"
	"net/url"
	"strings"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/rest"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

// AccountClient — SPOT account sub-client.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// rawBalanceDetail — raw format of a per-currency element from /account/balance.
// Full set of fields; see spot/types/aliases.go (Balance = swaptypes.Balance).
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

// rawBalance — raw format of the top-level element from /account/balance.
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
GetBalance returns the unified-account balance. If currencies are provided, the
OKX response is filtered by them (ccy=BTC,USDT query parameter). Without arguments,
all currencies with any balance/position on the account are returned.

ENDPOINT: GET /api/v5/account/balance[?ccy=BTC,USDT]
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

// rawOpenOrderEntry — raw response from /trade/orders-pending. OKX uses a
// single format for SPOT/SWAP; parsing is identical.
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
GetOpenOrders returns all active orders for a SPOT instrument.
OKX returns up to 100 orders per call; pagination is not implemented (see SWAP comment).
*/
func (a *AccountClient) GetOpenOrders(ctx context.Context, instID string) ([]types.OrderInfo, error) {
	if instID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "account.GetOpenOrders: InstID is empty", nil)
	}

	var q url.Values = url.Values{}
	q.Set("instType", string(types.InstTypeSpot))
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
