/*
ФАЙЛ: swap/trading.go

ОПИСАНИЕ:
Доменный саб-клиент торговли для SWAP-профиля OKX. Реализует операции:
  - CreateOrder        : POST /api/v5/trade/order
  - ModifyOrder        : POST /api/v5/trade/amend-order
  - CancelOrder        : POST /api/v5/trade/cancel-order
  - CreateBatchOrders  : POST /api/v5/trade/batch-orders        (до 20 за вызов)
  - ModifyBatchOrders  : POST /api/v5/trade/amend-batch-orders  (до 20)
  - CancelBatchOrders  : POST /api/v5/trade/cancel-batch-orders (до 20)
  - CancelAllOrders    : пакетная отмена через cancel-batch-orders на список open-orders
  - CancelForgottenOrders: отмена ордеров старше TTL.

ОСОБЕННОСТИ OKX:
  - У OKX нет «глобального CancelAll» по инструменту в стиле Binance — мы
    эмулируем его через GetOpenOrders + CancelBatchOrders.
  - Batch size — 20 (см. OKX docs §Trade API). Делим вход на чанки.
  - tdMode выводится из CreateOrderRequest:
      • TdMode явно задан → используется как есть;
      • TdMode пуст      → берётся cfg.DefaultTdMode (см. options);
      • если cfg.DefaultTdMode тоже пуст → cross (USD-M SWAP стандарт).
  - posSide для net-mode (default) всегда "net"; для long_short_mode пользователь
    обязан выставить request.PosSide явно.
  - clOrdId длина проверяется на стадии сборки запроса (1..32 символа [A-Za-z0-9_]).

ВНУТРЕННЕЕ СОСТОЯНИЕ:
  - clOrdToOrdID/ordIDToClOrd: маппинги ID. Полностью аналог Binance-коннектора,
    но без анти-паттернов из core: удаление маппинга — синхронно с cancel/filled.

ЗАВИСИМОСТИ:
- internal/rest: транспорт.
- swap/types:    доменные структуры.
- "github.com/tonymontanov/go-okx/v2": ошибки.
*/

package swap

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
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// MaxBatchSize — лимит OKX на batch trade endpoint'ы.
const MaxBatchSize = 20

// uniqSortedInstIDsCreate — отсортированный уникальный set InstID из батча
// CreateOrderRequest. Заполняем RequestMeta.Symbols для observer'а: внешнему
// rate-limiter'у важно знать каким именно символам списать usage из бюджета
// per (UID + InstId), а не блочить все символы из-за общего endpoint-счётчика.
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

// uniqSortedInstIDsModify — то же для ModifyOrderRequest.
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

// uniqSortedInstIDsCancel — то же для CancelOrderRequest.
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

// clOrdIDPattern — допустимые символы и длина clOrdId (см. OKX docs).
// OKX требует case-sensitive alphanumerics, БЕЗ подчёркиваний и других символов;
// длина 1..32. Любые '_', '-', '.' в clOrdId биржа отклоняет кодом 51000.
var clOrdIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

// TradingClient — саб-клиент торговли.
type TradingClient struct {
	c *Client

	mu          sync.RWMutex
	clOrdToOrd  map[string]string
	ordToClOrd  map[string]string
	createdAtMs map[string]int64 // clOrdId -> ms, для CancelForgottenOrders
}

func newTradingClient(c *Client) *TradingClient {
	return &TradingClient{
		c:           c,
		clOrdToOrd:  make(map[string]string, 1024),
		ordToClOrd:  make(map[string]string, 1024),
		createdAtMs: make(map[string]int64, 1024),
	}
}

/*
CreateOrder создаёт ордер SWAP.

Параметры:
  - ctx: контекст с дедлайном (см. ТЗ §6 — отмена обязана работать).
  - req: параметры ордера. Должен быть задан хотя бы InstID/Side/Size; цена
    обязательна для всех OrderType кроме market.

Возвращает:
  - OrderInfo с заполненным OrderID/ClientOrderID/CreatedAtMs/RateLimits.
  - *okx.Error при ошибке. Ошибки валидации SDK возвращаются БЕЗ запроса к OKX.

Поведение по tdMode/posSide см. в комментариях файла.
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

// buildCreateOrderBody собирает map[string]any для запроса OKX. Используем
// map (а не отдельную struct) сознательно: OKX игнорирует поля с пустыми
// строками, но struct с omitempty потребовал бы тегов под каждый тип данных.
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
	if orderType != types.OrderTypeMarket && orderType != types.OrderTypeOptimalLimitIOC && req.Price.IsZero() {
		return nil, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CreateOrder: Price is required for non-market order", nil)
	}

	var tdMode types.TdMode = req.TdMode
	if tdMode == "" {
		tdMode = types.TdModeCross
	}

	var posSide types.PosSide = req.PosSide
	if posSide == "" {
		posSide = types.PosSideNet
	}

	var body map[string]any = make(map[string]any, 12)
	body["instId"] = req.InstID
	body["tdMode"] = string(tdMode)
	body["side"] = string(req.Side)
	body["ordType"] = string(orderType)
	body["sz"] = req.Size.String()
	body["posSide"] = string(posSide)

	if orderType != types.OrderTypeMarket && orderType != types.OrderTypeOptimalLimitIOC {
		body["px"] = req.Price.String()
	}
	if req.ClientOrderID != "" {
		body["clOrdId"] = req.ClientOrderID
	}
	if req.ReduceOnly {
		body["reduceOnly"] = true
	}
	if req.Ccy != "" {
		body["ccy"] = req.Ccy
	}
	if req.Tag != "" {
		body["tag"] = req.Tag
	}

	return body, nil
}

// orderTypeFromTIF маппит TIF в OrderType OKX.
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

// orderActionResponseEntry — структура отдельного результата в массиве data
// для эндпоинтов order/amend-order/cancel-order и их batch-вариантов.
type orderActionResponseEntry struct {
	OrdID   string `json:"ordId"`
	ClOrdID string `json:"clOrdId"`
	Tag     string `json:"tag"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

// buildAmendOrderBody — общий конструктор тела amend-order (REST и WS).
// Валидация одинакова для обоих транспортов, поэтому вынесена в одну
// функцию: пользователь получит идентичные типизированные ошибки
// независимо от того, через какой sub-client отправил amend.
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
// транспортного уровня (chunk не успел уйти).
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
ModifyOrder делает amend ордера. У OKX можно поменять только sz/px; side/type
не меняются (нужно пересоздавать ордер).

Параметры:
  - req: должен быть задан ровно один идентификатор (OrderID или ClientOrderID)
    и хотя бы одно из NewSize/NewPrice.
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
CreateBatchOrders создаёт пакет ордеров. Сам OKX endpoint принимает до 20
ордеров за вызов; если вход больше — отрезаем чанками. Возвращает
последовательность OrderInfo в том же порядке, в котором были переданы
requests. Для отдельных ордеров, на которые OKX ответил sCode != "0", в
соответствующей позиции возвращается OrderInfo с заполненным State="canceled"
(условно) и пустыми OrderID/ClientOrderID — это позволяет вызывающему коду
обнаружить частичный успех. В то же время возвращается агрегированная ошибка
(errors.Join всех sCode != "0"), чтобы вызывающий код по err мог принять
решение.
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
			// OrderCount = реально отправленных в OKX (без invalid'ов,
			// которые мы отфильтровали в bodyErrs). Это списываем из
			// бюджета "300 orders per 2s" / "1000 new+amend / 2s".
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

// placeholderInfos строит «нулевые» OrderInfo для случая, когда мы вообще не
// смогли выполнить запрос (или собрать body) — чтобы вызывающий код мог
// сохранить порядок и понимать, какие ордера НЕ были отправлены.
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
CancelAllOrders отменяет ВСЕ активные ордера по инструменту. У OKX нет
эндпоинта «cancel-all-by-instrument», поэтому реализуем его через:
  GetOpenOrders → CancelBatchOrders(chunks).

Возвращает nil если ордеров не было.
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
CancelForgottenOrders — отменяет ордера старше maxAge. Использует свойство
OrderInfo.CreatedAtMs из ответа GetOpenOrders.
Возвращает список отменённых ордеров.
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

/*
CancelAllAfter вооружает (или disarm-ит) серверный таймер dead-man's
switch'а: если в течение `timeout` секунд клиент не вызовет endpoint
снова, OKX автоматически отменит ВСЕ открытые ордера учётки (всех
инструментов — не только SWAP).

ПАРАМЕТРЫ:
  - timeout > 0  (10..120s по spec OKX): арм. TriggerTimeMs в ответе —
    момент, когда биржа применит cancel-all-если-нет-refresh.
  - timeout == 0: disarm. TriggerTimeMs в ответе = 0.

ИСПОЛЬЗОВАНИЕ В HFT:
В hot-loop стратегии вызывайте каждые ~⅓ timeout (например, для
timeout=30s — каждые 10s). Это даёт большой запас на сетевые задержки.

ОТНОШЕНИЕ К MASSCANCEL:
mass-cancel (WS, Phase 2.1) — синхронный panic-button «отмени всё
сейчас». cancel-all-after — асинхронный «отмени всё через N секунд,
если я не обновлю». Используются совместно: arm на старте, mass-cancel
или disarm на graceful shutdown.
*/
func (t *TradingClient) CancelAllAfter(ctx context.Context, timeout time.Duration) (types.CancelAllAfterResult, error) {
	var out types.CancelAllAfterResult
	if timeout < 0 {
		return out, okx.NewError(okx.ErrorKindInvalidRequest, "", "trading.CancelAllAfter: timeout must be >= 0", nil)
	}
	var seconds int64 = int64(timeout / time.Second)
	var body map[string]any = map[string]any{
		"timeOut": fmt.Sprintf("%d", seconds),
	}

	var resp rest.Response
	var err error
	resp, _, err = t.c.rest().Do(ctx, rest.Options{
		Method: "POST",
		Path:   "/api/v5/trade/cancel-all-after",
		Body:   body,
		Signed: true,
		Meta: rest.RequestMeta{
			Symbols:  nil,
			Category: string(okx.RateLimitCategoryCancel),
		},
	})
	if err != nil {
		return out, err
	}

	type rawEntry struct {
		TriggerTime string `json:"triggerTime"`
		Ts          string `json:"ts"`
	}
	var entries []rawEntry
	if err = resp.UnmarshalData(&entries); err != nil {
		return out, okx.NewError(okx.ErrorKindUnknown, "", "trading.CancelAllAfter: parse", err)
	}
	if len(entries) == 0 {
		return out, nil
	}
	var e rawEntry = entries[0]
	if e.TriggerTime != "" {
		out.TriggerTimeMs, _ = parseInt64Lossy(e.TriggerTime)
	}
	if e.Ts != "" {
		out.TsMs, _ = parseInt64Lossy(e.Ts)
	}
	return out, nil
}

// parseInt64Lossy парсит строку OKX-таймштампа в int64; при ошибке
// возвращает 0. Используется только для безопасного парсинга числовых
// строк, для которых ошибка не критична (не блокирующая контракт).
func parseInt64Lossy(s string) (int64, error) {
	var n int64
	var i int
	for i = 0; i < len(s); i++ {
		var c byte = s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid digit %q in %q", c, s)
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
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

// SyncOrderMappings перезагружает маппинги из открытых ордеров на бирже —
// аналог Binance SyncOrderMappings. Удаляет из локальной мапы ордера, которых
// нет на бирже.
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

// ensureSigned — sanity-check: возвращает ошибку, если signer выключен.
// Используется в приватных эндпоинтах для понятной диагностики.
func (t *TradingClient) ensureSigned() error {
	if !t.c.signerEnabled() {
		return okx.NewError(okx.ErrorKindAuth, "", "trading: APIKey/SecretKey/Passphrase not configured", nil)
	}
	return nil
}

// _ ссылка на ensureSigned, чтобы не получать "declared and not used" пока
// мы не подключим её во все приватные методы. Будет удалена в M5 одновременно
// с подключением пред-проверки во все методы.
var _ = (*TradingClient).ensureSigned
