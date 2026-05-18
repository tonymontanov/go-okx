/*
ФАЙЛ: spot/trading.go

ОПИСАНИЕ:
Доменный саб-клиент торговли для SPOT-профиля OKX. Эндпоинты OKX v5 общие
для SPOT и SWAP — отличается только содержимое тела запроса:
  - tdMode по умолчанию "cash" (на SPOT cross/isolated — это spot margin
    trading, в текущей версии SDK не используется по умолчанию).
  - НЕТ posSide.
  - НЕТ reduceOnly.
  - Для market BUY доступен tgtCcy (base_ccy/quote_ccy) — см. doc.
  - sz измеряется в БАЗОВОЙ валюте (либо в quote_ccy для market BUY
    при tgtCcy="quote_ccy").

ИНВАРИАНТЫ:
  - clOrdId [A-Za-z0-9]{1,32} (как и в SWAP — то же ограничение OKX).
  - Batch size — 20 (как в SWAP).
  - У OKX нет глобального cancel-all-by-instrument; эмулируем через
    GetOpenOrders + CancelBatchOrders.

ВНУТРЕННЕЕ СОСТОЯНИЕ:
  - clOrdToOrd/ordToClOrd: те же маппинги ID, что и в SWAP.

ЗАВИСИМОСТИ:
  - internal/rest: транспорт.
  - spot/types:    доменные структуры SPOT.
  - github.com/tonymontanov/go-okx/v2: ошибки и категории rate-limit.
*/

package spot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/rest"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

// MaxBatchSize — лимит OKX на batch trade endpoint'ы (одинаков для SPOT/SWAP).
const MaxBatchSize = 20

// clOrdIDPattern — допустимые символы и длина clOrdId.
// OKX: case-sensitive alphanumerics [A-Za-z0-9]{1,32}; '_', '-', '.' отклоняются
// кодом 51000.
var clOrdIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

// TradingClient — саб-клиент торговли SPOT.
type TradingClient struct {
	c *Client

	mu          sync.RWMutex
	clOrdToOrd  map[string]string
	ordToClOrd  map[string]string
	createdAtMs map[string]int64
}

func newTradingClient(c *Client) *TradingClient {
	return &TradingClient{
		c:           c,
		clOrdToOrd:  make(map[string]string, 1024),
		ordToClOrd:  make(map[string]string, 1024),
		createdAtMs: make(map[string]int64, 1024),
	}
}

// orderActionResponseEntry — структура отдельного результата для всех
// trade/* endpoint'ов (один формат у OKX).
type orderActionResponseEntry struct {
	OrdID   string `json:"ordId"`
	ClOrdID string `json:"clOrdId"`
	Tag     string `json:"tag"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

// uniqSortedInstIDsCreate — уникальный отсортированный set InstID из батча,
// нужен для RateLimitEvent.Symbols (внешний rate-limiter моделирует лимиты
// per (UID + InstId)).
func uniqSortedInstIDsCreate(chunk []types.CreateOrderRequest) []string {
	if len(chunk) == 0 {
		return nil
	}
	var set map[string]struct{} = make(map[string]struct{}, len(chunk))
	var i int
	for i = 0; i < len(chunk); i++ {
		if chunk[i].InstID == "" {
			continue
		}
		set[chunk[i].InstID] = struct{}{}
	}
	return setToSortedSlice(set)
}

func uniqSortedInstIDsModify(chunk []types.ModifyOrderRequest) []string {
	if len(chunk) == 0 {
		return nil
	}
	var set map[string]struct{} = make(map[string]struct{}, len(chunk))
	var i int
	for i = 0; i < len(chunk); i++ {
		if chunk[i].InstID == "" {
			continue
		}
		set[chunk[i].InstID] = struct{}{}
	}
	return setToSortedSlice(set)
}

func uniqSortedInstIDsCancel(chunk []types.CancelOrderRequest) []string {
	if len(chunk) == 0 {
		return nil
	}
	var set map[string]struct{} = make(map[string]struct{}, len(chunk))
	var i int
	for i = 0; i < len(chunk); i++ {
		if chunk[i].InstID == "" {
			continue
		}
		set[chunk[i].InstID] = struct{}{}
	}
	return setToSortedSlice(set)
}

func setToSortedSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	var out []string = make([]string, 0, len(set))
	var s string
	for s = range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// orderTypeFromTIF маппит TIF в OrderType OKX. Идентично swap (один и тот же
// enum, см. spot/types/aliases.go).
func orderTypeFromTIF(tif types.TimeInForceType) types.OrderType {
	switch tif {
	case types.TimeInForceTypeIOC:
		return types.OrderTypeIOC
	case types.TimeInForceTypeFOK:
		return types.OrderTypeFOK
	case types.TimeInForceTypeGTX:
		return types.OrderTypePostOnly
	case types.TimeInForceTypeGTC, "":
		return types.OrderTypeLimit
	default:
		return types.OrderTypeLimit
	}
}

/*
CreateOrder создаёт ордер SPOT.

Параметры:
  - ctx: контекст с дедлайном.
  - req: параметры ордера. InstID/Side/Size — обязательны; Price обязателен
    для всех OrderType кроме market.

Возвращает OrderInfo с заполненным OrderID/ClientOrderID/CreatedAtMs/RateLimits.
*/
func (t *TradingClient) CreateOrder(ctx context.Context, req types.CreateOrderRequest) (types.OrderInfo, error) {
	var info types.OrderInfo
	var err error

	var body map[string]any
	body, err = t.buildCreateOrderBody(req)
	if err != nil {
		return info, err
	}

	var resp rest.Response
	var rateLimits map[string]string
	resp, rateLimits, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/trade/order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: 1,
			Symbols:    []string{req.InstID},
			Category:   string(okx.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return info, err
	}

	var entries []orderActionResponseEntry
	if err = resp.UnmarshalData(&entries); err != nil {
		return info, okx.NewError(okx.ErrorKindUnknown, "", "trading.CreateOrder: parse", err)
	}
	if len(entries) == 0 {
		return info, okx.NewError(okx.ErrorKindExchange, "", "trading.CreateOrder: empty data (top-level code="+resp.Code+", msg="+resp.Msg+")", nil)
	}
	var e orderActionResponseEntry = entries[0]
	if e.SCode != "" && e.SCode != "0" {
		return info, &okx.Error{
			Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
			OKXCode: e.SCode,
			Message: e.SMsg,
		}
	}

	info = types.OrderInfo{
		OrderID:       e.OrdID,
		ClientOrderID: e.ClOrdID,
		InstID:        req.InstID,
		Side:          req.Side,
		OrderType:     req.OrderType,
		Price:         req.Price,
		Size:          req.Size,
		State:         types.OrderStateLive,
		CreatedAtMs:   time.Now().UnixMilli(),
		RateLimits:    rateLimits,
	}
	t.rememberMapping(e.ClOrdID, e.OrdID, info.CreatedAtMs)
	return info, nil
}

// buildCreateOrderBody собирает map[string]any для создания SPOT-ордера.
func (t *TradingClient) buildCreateOrderBody(req types.CreateOrderRequest) (map[string]any, error) {
	if req.InstID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CreateOrder: InstID is empty", nil)
	}
	if req.Side == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CreateOrder: Side is empty", nil)
	}
	if req.Size.IsZero() {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CreateOrder: Size is zero", nil)
	}
	if req.ClientOrderID != "" && !clOrdIDPattern.MatchString(req.ClientOrderID) {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CreateOrder: invalid ClientOrderID (1..32 chars of [A-Za-z0-9], no underscores or punctuation)", nil)
	}

	var orderType types.OrderType = req.OrderType
	if orderType == "" {
		orderType = orderTypeFromTIF(req.TimeInForce)
	}
	if orderType != types.OrderTypeMarket && req.Price.IsZero() {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CreateOrder: Price is required for non-market order", nil)
	}

	var tdMode types.TdMode = req.TdMode
	if tdMode == "" {
		tdMode = types.TdModeCash
	}

	var body map[string]any = make(map[string]any, 12)
	body["instId"] = req.InstID
	body["tdMode"] = string(tdMode)
	body["side"] = string(req.Side)
	body["ordType"] = string(orderType)
	body["sz"] = req.Size.String()

	if orderType != types.OrderTypeMarket {
		body["px"] = req.Price.String()
	}
	if req.ClientOrderID != "" {
		body["clOrdId"] = req.ClientOrderID
	}
	// tgtCcy актуален только для market — для limit OKX игнорирует поле,
	// но явно не отправляем, чтобы не плодить лишних полей.
	if orderType == types.OrderTypeMarket && req.TgtCcy != "" {
		body["tgtCcy"] = string(req.TgtCcy)
	}
	if req.Ccy != "" {
		body["ccy"] = req.Ccy
	}
	if req.Tag != "" {
		body["tag"] = req.Tag
	}

	return body, nil
}

// buildAmendOrderBody — общий конструктор тела для amend-order (одиночный
// и batch варианты, REST и WS). Валидация вынесена сюда, чтобы WS и REST
// возвращали одинаковые типизированные ошибки на одинаковые входные данные.
func buildAmendOrderBody(req types.ModifyOrderRequest) (map[string]any, error) {
	if req.InstID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.ModifyOrder: InstID is empty", nil)
	}
	if (req.OrderID == "" && req.ClientOrderID == "") || (req.OrderID != "" && req.ClientOrderID != "") {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.ModifyOrder: exactly one of OrderID/ClientOrderID must be set", nil)
	}
	if req.NewSize.IsZero() && req.NewPrice.IsZero() {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.ModifyOrder: NewSize or NewPrice must be set", nil)
	}
	var body map[string]any = make(map[string]any, 6)
	body["instId"] = req.InstID
	if req.OrderID != "" {
		body["ordId"] = req.OrderID
	}
	if req.ClientOrderID != "" {
		body["clOrdId"] = req.ClientOrderID
	}
	if !req.NewSize.IsZero() {
		body["newSz"] = req.NewSize.String()
	}
	if !req.NewPrice.IsZero() {
		body["newPx"] = req.NewPrice.String()
	}
	if req.RequestID != "" {
		body["reqId"] = req.RequestID
	}
	return body, nil
}

// buildCancelOrderBody — общий конструктор тела cancel-order (REST и WS).
func buildCancelOrderBody(req types.CancelOrderRequest) (map[string]any, error) {
	if req.InstID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CancelOrder: InstID is empty", nil)
	}
	if (req.OrderID == "" && req.ClientOrderID == "") || (req.OrderID != "" && req.ClientOrderID != "") {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CancelOrder: exactly one of OrderID/ClientOrderID must be set", nil)
	}
	var body map[string]any = map[string]any{"instId": req.InstID}
	if req.OrderID != "" {
		body["ordId"] = req.OrderID
	}
	if req.ClientOrderID != "" {
		body["clOrdId"] = req.ClientOrderID
	}
	return body, nil
}

// placeholderInfosModify — REST/WS-симметричная заглушка при ошибке
// transport-уровня (chunk не успел уйти).
func placeholderInfosModify(chunk []types.ModifyOrderRequest) []types.OrderInfo {
	var out []types.OrderInfo = make([]types.OrderInfo, 0, len(chunk))
	var i int
	for i = 0; i < len(chunk); i++ {
		out = append(out, types.OrderInfo{
			InstID:        chunk[i].InstID,
			ClientOrderID: chunk[i].ClientOrderID,
			Price:         chunk[i].NewPrice,
			Size:          chunk[i].NewSize,
			State:         types.OrderStateUnknown,
		})
	}
	return out
}

/*
ModifyOrder — amend ордера. OKX меняет только sz/px; side/type — нет.
*/
func (t *TradingClient) ModifyOrder(ctx context.Context, req types.ModifyOrderRequest) (types.OrderInfo, error) {
	var info types.OrderInfo
	var err error

	var body map[string]any
	body, err = buildAmendOrderBody(req)
	if err != nil {
		return info, err
	}

	var resp rest.Response
	var rateLimits map[string]string
	resp, rateLimits, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/trade/amend-order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: 1,
			Symbols:    []string{req.InstID},
			Category:   string(okx.RateLimitCategoryAmend),
		},
	})
	if err != nil {
		return info, err
	}

	var entries []orderActionResponseEntry
	if err = resp.UnmarshalData(&entries); err != nil {
		return info, okx.NewError(okx.ErrorKindUnknown, "", "trading.ModifyOrder: parse", err)
	}
	if len(entries) == 0 {
		return info, okx.NewError(okx.ErrorKindExchange, "", "trading.ModifyOrder: empty data", nil)
	}
	var e orderActionResponseEntry = entries[0]
	if e.SCode != "" && e.SCode != "0" {
		return info, &okx.Error{
			Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
			OKXCode: e.SCode,
			Message: e.SMsg,
		}
	}

	info = types.OrderInfo{
		OrderID:       e.OrdID,
		ClientOrderID: e.ClOrdID,
		InstID:        req.InstID,
		Price:         req.NewPrice,
		Size:          req.NewSize,
		State:         types.OrderStateLive,
		UpdatedAtMs:   time.Now().UnixMilli(),
		RateLimits:    rateLimits,
	}
	return info, nil
}

/*
CancelOrder отменяет один ордер. Должен быть задан ровно один из идентификаторов.
*/
func (t *TradingClient) CancelOrder(ctx context.Context, req types.CancelOrderRequest) error {
	var body map[string]any
	var err error
	body, err = buildCancelOrderBody(req)
	if err != nil {
		return err
	}

	var resp rest.Response
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/trade/cancel-order",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: 1,
			Symbols:    []string{req.InstID},
			Category:   string(okx.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return err
	}

	var entries []orderActionResponseEntry
	if err = resp.UnmarshalData(&entries); err != nil {
		return okx.NewError(okx.ErrorKindUnknown, "", "trading.CancelOrder: parse", err)
	}
	if len(entries) == 0 {
		return okx.NewError(okx.ErrorKindExchange, "", "trading.CancelOrder: empty data", nil)
	}
	var e orderActionResponseEntry = entries[0]
	if e.SCode != "" && e.SCode != "0" {
		return &okx.Error{
			Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
			OKXCode: e.SCode,
			Message: e.SMsg,
		}
	}
	t.forgetMappingByClOrdOrOrd(req.ClientOrderID, req.OrderID)
	return nil
}

/*
CreateBatchOrders создаёт пакет ордеров. До 20 за чанк. Возврат семантически
тот же, что и в swap: позиция в выходном слайсе совпадает с позицией во входном,
ошибки агрегируются через errors.Join.
*/
func (t *TradingClient) CreateBatchOrders(ctx context.Context, reqs []types.CreateOrderRequest) ([]types.OrderInfo, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	var out []types.OrderInfo = make([]types.OrderInfo, 0, len(reqs))
	var aggErrs []error
	var err error

	var chunkStart int
	for chunkStart = 0; chunkStart < len(reqs); chunkStart += MaxBatchSize {
		var chunkEnd int = chunkStart + MaxBatchSize
		if chunkEnd > len(reqs) {
			chunkEnd = len(reqs)
		}
		var chunk []types.CreateOrderRequest = reqs[chunkStart:chunkEnd]
		var infos []types.OrderInfo
		infos, err = t.createBatchChunk(ctx, chunk)
		out = append(out, infos...)
		if err != nil {
			aggErrs = append(aggErrs, err)
		}
	}

	if len(aggErrs) > 0 {
		return out, errors.Join(aggErrs...)
	}
	return out, nil
}

func (t *TradingClient) createBatchChunk(ctx context.Context, chunk []types.CreateOrderRequest) ([]types.OrderInfo, error) {
	var bodies []map[string]any = make([]map[string]any, 0, len(chunk))
	var bodyErrs []error
	var err error
	var i int
	for i = 0; i < len(chunk); i++ {
		var b map[string]any
		b, err = t.buildCreateOrderBody(chunk[i])
		if err != nil {
			bodyErrs = append(bodyErrs, fmt.Errorf("batch[%d]: %w", i, err))
			continue
		}
		bodies = append(bodies, b)
	}
	if len(bodies) == 0 {
		return placeholderInfos(chunk), errors.Join(bodyErrs...)
	}

	var resp rest.Response
	var rateLimits map[string]string
	resp, rateLimits, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/trade/batch-orders",
		Body:   bodies,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: len(bodies),
			Symbols:    uniqSortedInstIDsCreate(chunk),
			Category:   string(okx.RateLimitCategoryPlace),
		},
	})
	if err != nil {
		return placeholderInfos(chunk), err
	}

	var entries []orderActionResponseEntry
	if err = resp.UnmarshalData(&entries); err != nil {
		return placeholderInfos(chunk), okx.NewError(okx.ErrorKindUnknown, "", "trading.CreateBatchOrders: parse", err)
	}

	var infos []types.OrderInfo = make([]types.OrderInfo, 0, len(chunk))
	var aggErrs []error = bodyErrs
	var now int64 = time.Now().UnixMilli()

	for i = 0; i < len(chunk); i++ {
		if i >= len(entries) {
			infos = append(infos, types.OrderInfo{
				InstID:        chunk[i].InstID,
				Side:          chunk[i].Side,
				Price:         chunk[i].Price,
				Size:          chunk[i].Size,
				ClientOrderID: chunk[i].ClientOrderID,
				State:         types.OrderStateUnknown,
			})
			continue
		}
		var e orderActionResponseEntry = entries[i]
		var info types.OrderInfo = types.OrderInfo{
			OrderID:       e.OrdID,
			ClientOrderID: e.ClOrdID,
			InstID:        chunk[i].InstID,
			Side:          chunk[i].Side,
			OrderType:     chunk[i].OrderType,
			Price:         chunk[i].Price,
			Size:          chunk[i].Size,
			State:         types.OrderStateLive,
			CreatedAtMs:   now,
			RateLimits:    rateLimits,
		}
		if e.SCode != "" && e.SCode != "0" {
			info.State = types.OrderStateUnknown
			aggErrs = append(aggErrs, &okx.Error{
				Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
				OKXCode: e.SCode,
				Message: e.SMsg,
			})
		} else {
			t.rememberMapping(e.ClOrdID, e.OrdID, now)
		}
		infos = append(infos, info)
	}

	if len(aggErrs) > 0 {
		return infos, errors.Join(aggErrs...)
	}
	return infos, nil
}

func placeholderInfos(chunk []types.CreateOrderRequest) []types.OrderInfo {
	var out []types.OrderInfo = make([]types.OrderInfo, 0, len(chunk))
	var i int
	for i = 0; i < len(chunk); i++ {
		out = append(out, types.OrderInfo{
			InstID:        chunk[i].InstID,
			Side:          chunk[i].Side,
			Price:         chunk[i].Price,
			Size:          chunk[i].Size,
			ClientOrderID: chunk[i].ClientOrderID,
			State:         types.OrderStateUnknown,
		})
	}
	return out
}

/*
ModifyBatchOrders — пакетный amend ордеров (до 20 за чанк).
*/
func (t *TradingClient) ModifyBatchOrders(ctx context.Context, reqs []types.ModifyOrderRequest) ([]types.OrderInfo, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	var out []types.OrderInfo = make([]types.OrderInfo, 0, len(reqs))
	var aggErrs []error
	var err error

	var chunkStart int
	for chunkStart = 0; chunkStart < len(reqs); chunkStart += MaxBatchSize {
		var chunkEnd int = chunkStart + MaxBatchSize
		if chunkEnd > len(reqs) {
			chunkEnd = len(reqs)
		}
		var infos []types.OrderInfo
		infos, err = t.modifyBatchChunk(ctx, reqs[chunkStart:chunkEnd])
		out = append(out, infos...)
		if err != nil {
			aggErrs = append(aggErrs, err)
		}
	}
	if len(aggErrs) > 0 {
		return out, errors.Join(aggErrs...)
	}
	return out, nil
}

func (t *TradingClient) modifyBatchChunk(ctx context.Context, chunk []types.ModifyOrderRequest) ([]types.OrderInfo, error) {
	var bodies []map[string]any = make([]map[string]any, 0, len(chunk))
	var bodyErrs []error
	var i int
	for i = 0; i < len(chunk); i++ {
		var r types.ModifyOrderRequest = chunk[i]
		if r.InstID == "" || (r.OrderID == "" && r.ClientOrderID == "") || (r.OrderID != "" && r.ClientOrderID != "") || (r.NewSize.IsZero() && r.NewPrice.IsZero()) {
			bodyErrs = append(bodyErrs, fmt.Errorf("batch[%d]: invalid amend request", i))
			continue
		}
		var b map[string]any = map[string]any{"instId": r.InstID}
		if r.OrderID != "" {
			b["ordId"] = r.OrderID
		}
		if r.ClientOrderID != "" {
			b["clOrdId"] = r.ClientOrderID
		}
		if !r.NewSize.IsZero() {
			b["newSz"] = r.NewSize.String()
		}
		if !r.NewPrice.IsZero() {
			b["newPx"] = r.NewPrice.String()
		}
		if r.RequestID != "" {
			b["reqId"] = r.RequestID
		}
		bodies = append(bodies, b)
	}
	if len(bodies) == 0 {
		return nil, errors.Join(bodyErrs...)
	}

	var resp rest.Response
	var rateLimits map[string]string
	var err error
	resp, rateLimits, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/trade/amend-batch-orders",
		Body:   bodies,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: len(bodies),
			Symbols:    uniqSortedInstIDsModify(chunk),
			Category:   string(okx.RateLimitCategoryAmend),
		},
	})
	if err != nil {
		return nil, err
	}

	var entries []orderActionResponseEntry
	if err = resp.UnmarshalData(&entries); err != nil {
		return nil, okx.NewError(okx.ErrorKindUnknown, "", "trading.ModifyBatchOrders: parse", err)
	}

	var infos []types.OrderInfo = make([]types.OrderInfo, 0, len(chunk))
	var aggErrs []error = bodyErrs
	var now int64 = time.Now().UnixMilli()

	for i = 0; i < len(chunk); i++ {
		if i >= len(entries) {
			infos = append(infos, types.OrderInfo{
				InstID:        chunk[i].InstID,
				ClientOrderID: chunk[i].ClientOrderID,
				OrderID:       chunk[i].OrderID,
				Price:         chunk[i].NewPrice,
				Size:          chunk[i].NewSize,
				State:         types.OrderStateUnknown,
			})
			continue
		}
		var e orderActionResponseEntry = entries[i]
		var info types.OrderInfo = types.OrderInfo{
			OrderID:       e.OrdID,
			ClientOrderID: e.ClOrdID,
			InstID:        chunk[i].InstID,
			Price:         chunk[i].NewPrice,
			Size:          chunk[i].NewSize,
			State:         types.OrderStateLive,
			UpdatedAtMs:   now,
			RateLimits:    rateLimits,
		}
		if e.SCode != "" && e.SCode != "0" {
			info.State = types.OrderStateUnknown
			aggErrs = append(aggErrs, &okx.Error{
				Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
				OKXCode: e.SCode,
				Message: e.SMsg,
			})
		}
		infos = append(infos, info)
	}
	if len(aggErrs) > 0 {
		return infos, errors.Join(aggErrs...)
	}
	return infos, nil
}

/*
CancelBatchOrders отменяет пакет ордеров (до 20 за чанк).
*/
func (t *TradingClient) CancelBatchOrders(ctx context.Context, reqs []types.CancelOrderRequest) error {
	if len(reqs) == 0 {
		return nil
	}
	var aggErrs []error
	var err error

	var chunkStart int
	for chunkStart = 0; chunkStart < len(reqs); chunkStart += MaxBatchSize {
		var chunkEnd int = chunkStart + MaxBatchSize
		if chunkEnd > len(reqs) {
			chunkEnd = len(reqs)
		}
		err = t.cancelBatchChunk(ctx, reqs[chunkStart:chunkEnd])
		if err != nil {
			aggErrs = append(aggErrs, err)
		}
	}
	if len(aggErrs) > 0 {
		return errors.Join(aggErrs...)
	}
	return nil
}

func (t *TradingClient) cancelBatchChunk(ctx context.Context, chunk []types.CancelOrderRequest) error {
	var bodies []map[string]any = make([]map[string]any, 0, len(chunk))
	var i int
	for i = 0; i < len(chunk); i++ {
		var r types.CancelOrderRequest = chunk[i]
		if r.InstID == "" || (r.OrderID == "" && r.ClientOrderID == "") || (r.OrderID != "" && r.ClientOrderID != "") {
			return okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CancelBatchOrders: invalid request", nil)
		}
		var b map[string]any = map[string]any{"instId": r.InstID}
		if r.OrderID != "" {
			b["ordId"] = r.OrderID
		}
		if r.ClientOrderID != "" {
			b["clOrdId"] = r.ClientOrderID
		}
		bodies = append(bodies, b)
	}

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/trade/cancel-batch-orders",
		Body:   bodies,
		Signed: true,
		Meta: rest.RequestMeta{
			OrderCount: len(bodies),
			Symbols:    uniqSortedInstIDsCancel(chunk),
			Category:   string(okx.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return err
	}

	var entries []orderActionResponseEntry
	if err = resp.UnmarshalData(&entries); err != nil {
		return okx.NewError(okx.ErrorKindUnknown, "", "trading.CancelBatchOrders: parse", err)
	}

	var aggErrs []error
	for i = 0; i < len(entries); i++ {
		if entries[i].SCode != "" && entries[i].SCode != "0" {
			aggErrs = append(aggErrs, &okx.Error{
				Kind:    okx.MapOKXCode(entries[i].SCode, entries[i].SMsg),
				OKXCode: entries[i].SCode,
				Message: entries[i].SMsg,
			})
			continue
		}
		t.forgetMappingByClOrdOrOrd(entries[i].ClOrdID, entries[i].OrdID)
	}
	if len(aggErrs) > 0 {
		return errors.Join(aggErrs...)
	}
	return nil
}

/*
CancelAllOrders отменяет ВСЕ активные ордера по инструменту через
GetOpenOrders → CancelBatchOrders. Зеркало SWAP-реализации.
*/
func (t *TradingClient) CancelAllOrders(ctx context.Context, instID string) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CancelAllOrders: InstID is empty", nil)
	}

	var open []types.OrderInfo
	var err error
	open, err = t.c.Account().GetOpenOrders(ctx, instID)
	if err != nil {
		return err
	}
	if len(open) == 0 {
		return nil
	}

	var reqs []types.CancelOrderRequest = make([]types.CancelOrderRequest, 0, len(open))
	var i int
	for i = 0; i < len(open); i++ {
		reqs = append(reqs, types.CancelOrderRequest{
			InstID:  instID,
			OrderID: open[i].OrderID,
		})
	}
	return t.CancelBatchOrders(ctx, reqs)
}

/*
CancelForgottenOrders отменяет ордера старше maxAge.
*/
func (t *TradingClient) CancelForgottenOrders(ctx context.Context, instID string, maxAge time.Duration) ([]types.OrderInfo, error) {
	if instID == "" {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CancelForgottenOrders: InstID is empty", nil)
	}
	if maxAge <= 0 {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CancelForgottenOrders: maxAge must be positive", nil)
	}

	var open []types.OrderInfo
	var err error
	open, err = t.c.Account().GetOpenOrders(ctx, instID)
	if err != nil {
		return nil, err
	}

	var now int64 = time.Now().UnixMilli()
	var thresholdMs int64 = now - maxAge.Milliseconds()

	var stale []types.OrderInfo
	var reqs []types.CancelOrderRequest
	var i int
	for i = 0; i < len(open); i++ {
		if open[i].CreatedAtMs > 0 && open[i].CreatedAtMs <= thresholdMs {
			stale = append(stale, open[i])
			reqs = append(reqs, types.CancelOrderRequest{InstID: instID, OrderID: open[i].OrderID})
		}
	}
	if len(reqs) == 0 {
		return nil, nil
	}
	err = t.CancelBatchOrders(ctx, reqs)
	return stale, err
}

// rememberMapping добавляет ClOrdID ↔ OrdID и фиксирует время создания.
func (t *TradingClient) rememberMapping(clOrdID, ordID string, createdAtMs int64) {
	if clOrdID == "" || ordID == "" {
		return
	}
	t.mu.Lock()
	t.clOrdToOrd[clOrdID] = ordID
	t.ordToClOrd[ordID] = clOrdID
	t.createdAtMs[clOrdID] = createdAtMs
	t.mu.Unlock()
}

// forgetMappingByClOrdOrOrd удаляет маппинг по любому из идентификаторов.
func (t *TradingClient) forgetMappingByClOrdOrOrd(clOrdID, ordID string) {
	if clOrdID == "" && ordID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if clOrdID == "" {
		clOrdID = t.ordToClOrd[ordID]
	}
	if ordID == "" {
		ordID = t.clOrdToOrd[clOrdID]
	}
	delete(t.clOrdToOrd, clOrdID)
	delete(t.ordToClOrd, ordID)
	delete(t.createdAtMs, clOrdID)
}

// OrderIDByClientID возвращает OrdID по ClOrdID, если он известен SDK.
func (t *TradingClient) OrderIDByClientID(clOrdID string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var v string
	var ok bool
	v, ok = t.clOrdToOrd[clOrdID]
	return v, ok
}

// ClientIDByOrderID возвращает ClOrdID по OrdID, если он известен SDK.
func (t *TradingClient) ClientIDByOrderID(ordID string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var v string
	var ok bool
	v, ok = t.ordToClOrd[ordID]
	return v, ok
}

/*
SyncOrderMappings перезагружает маппинги из открытых ордеров на бирже.
Удаляет из локальной мапы ордера, которых нет в ответе биржи.
*/
func (t *TradingClient) SyncOrderMappings(ctx context.Context, instID string) error {
	if instID == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.SyncOrderMappings: InstID is empty", nil)
	}
	var open []types.OrderInfo
	var err error
	open, err = t.c.Account().GetOpenOrders(ctx, instID)
	if err != nil {
		return err
	}

	var seen map[string]struct{} = make(map[string]struct{}, len(open))
	var i int
	for i = 0; i < len(open); i++ {
		if open[i].ClientOrderID != "" && open[i].OrderID != "" {
			t.rememberMapping(open[i].ClientOrderID, open[i].OrderID, open[i].CreatedAtMs)
			seen[open[i].ClientOrderID] = struct{}{}
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	var clOrdID string
	var ordID string
	for clOrdID, ordID = range t.clOrdToOrd {
		if _, ok := seen[clOrdID]; !ok {
			delete(t.clOrdToOrd, clOrdID)
			delete(t.ordToClOrd, ordID)
			delete(t.createdAtMs, clOrdID)
		}
	}
	return nil
}
