/*
ФАЙЛ: spot/account.go

ОПИСАНИЕ:
Доменный саб-клиент аккаунта/балансов для SPOT-профиля OKX.

Реализованные методы:
  - GetBalance      : GET /api/v5/account/balance[?ccy=...]
  - GetOpenOrders   : GET /api/v5/trade/orders-pending?instType=SPOT&instId=...

ЧЕГО ЗДЕСЬ НЕТ И ПОЧЕМУ:
  - GetPositions / GetSymbolPosition: на спот OKX позиций НЕ возвращает
    через /account/positions. Состояние "что у меня есть" на cash-споте —
    это просто Balance.Details. Поэтому в spot/account.go этих методов нет;
    спотовый коннектор в торговом ядре строит types.PositionInfo на основе
    free-balance базовой валюты (см. core/internal/connectors/okx/spot).
  - SetLeverage / SetPositionMode: на cash-споте не применимы. Если в
    будущем добавим spot margin (cross/isolated) — методы пойдут в отдельный
    spot/margin/* подпакет, чтобы не загромождать API cash-only сценариям.
  - ClosePosition: на cash-споте отсутствует понятие "позиция к закрытию".

ENDPOINT БАЛАНСА ОБЩИЙ У SPOT И SWAP:
OKX использует unified-account — баланс не разделён по instType. Поэтому
парсинг точно такой же, как в swap/account.go, и тип Balance общий
(см. spot/types/aliases.go → Balance = swaptypes.Balance).
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

// AccountClient — саб-клиент аккаунта SPOT.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// rawBalanceDetail — сырой формат per-currency элемента из /account/balance.
// Полный набор полей; см. spot/types/aliases.go (Balance = swaptypes.Balance).
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

// rawBalance — сырой формат top-level элемента из /account/balance.
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
GetBalance возвращает unified-account баланс. Если переданы валюты — фильтрует
ответ OKX по ним (передаётся параметр ccy=BTC,USDT). Без аргументов вернутся
все валюты, по которым у аккаунта есть какой-либо баланс/позиция.

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

// rawOpenOrderEntry — сырой ответ /trade/orders-pending. Один формат у OKX
// для SPOT/SWAP, parse одинаковый.
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
GetOpenOrders возвращает все активные ордера по SPOT-инструменту.
OKX возвращает до 100 ордеров за вызов; пагинацию не делаем (см. SWAP-комментарий).
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
