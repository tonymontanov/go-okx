/*
ФАЙЛ: swap/account.go

ОПИСАНИЕ:
Доменный саб-клиент аккаунта/позиций SWAP. Реализует:
  - GetBalance                       : GET /api/v5/account/balance[?ccy=...]
  - GetPosition / GetSymbolPosition  : GET /api/v5/account/positions?instType=SWAP&instId=...
  - GetOpenOrders                    : GET /api/v5/trade/orders-pending?instType=SWAP&instId=...
  - ClosePosition                    : POST /api/v5/trade/close-position (market close)
  - SetLeverage                      : POST /api/v5/account/set-leverage
  - SetPositionMode                  : POST /api/v5/account/set-position-mode

ОСОБЕННОСТИ OKX:
  - В net-mode (default) у инструмента может быть максимум одна позиция:
    знак Position указывает на сторону (+long/-short). PosSide всегда "net".
  - В hedge-mode (long_short_mode) у одного инструмента может быть до двух
    позиций (long + short). В v1 мы возвращаем массив; helper GetSymbolPosition
    с режимом по умолчанию (net) выбирает первую запись.
  - ClosePosition позволяет закрыть рыночно — без необходимости знать size.
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

// AccountClient — саб-клиент аккаунта/позиций.
type AccountClient struct {
	c *Client
}

func newAccountClient(c *Client) *AccountClient {
	return &AccountClient{c: c}
}

// rawBalanceDetail — сырой формат per-currency элемента из /account/balance.
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

ВНИМАНИЕ:
  - Эндпоинт возвращает массив из ровно одного элемента (по дизайну OKX).
  - В Demo-режиме (Config.Demo=true) баланс возвращается из песочницы, а не
    из реального портфеля.
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
			// Symbols пустой: balance не per-instrument.
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

// rawPositionEntry — сырой ответ /account/positions (только используемые поля).
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
GetPositions возвращает все открытые позиции по SWAP-инструменту (1 в net-mode,
до 2 в hedge-mode). Если позиции нет — пустой слайс, не ошибка.
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
GetSymbolPosition возвращает позицию для одного инструмента в net-mode (или
первую, если по какой-то причине пришло несколько). В hedge-mode используйте
GetPositions и фильтруйте по PosSide самостоятельно.
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

// rawOpenOrderEntry — сырой ответ /trade/orders-pending. Поля совпадают с
// /trade/order-history (используется как структура и там).
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
GetOpenOrders возвращает все активные ордера по инструменту. OKX возвращает до
100 за вызов, в SDK мы НЕ делаем пагинацию — в реальном HFT-сценарии 100
открытых ордеров на инструмент это уже много; если нужно больше, добавим
итерацию по beforeID позже.
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
ClosePosition закрывает позицию рыночно. Использует
POST /api/v5/trade/close-position. mgnMode по умолчанию cross.

Если позиции нет — OKX вернёт ошибку 51400/51169; мы возвращаем её как есть,
вызывающий код решает, является ли это «нормальной» ситуацией.
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
			// close-position шлёт market-order под капотом — учитываем
			// как Place (списываем из sub-account 1000/2s бюджета и из
			// per-symbol budget). OrderCount=1 потому что один market-close.
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
SetLeverage устанавливает плечо для инструмента и mgnMode. По умолчанию для
SWAP — cross. В hedge-mode (long_short_mode) дополнительно требуется posSide;
SDK здесь устанавливает плечо для обеих сторон.
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
SetPositionMode задаёт режим позиций аккаунта (net_mode / long_short_mode).
Действует на весь аккаунт. SDK по умолчанию ориентирован на net_mode.
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
			// set-position-mode account-wide, instId не передаётся.
			Category: string(okx.RateLimitCategoryQuery),
		},
	})
	if err != nil {
		return err
	}
	_ = resp
	return nil
}
