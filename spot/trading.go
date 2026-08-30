/*
FILE: spot/trading.go

DESCRIPTION:
Domain sub-client for SPOT profile trading. OKX v5 endpoints are shared
between SPOT and SWAP — only the request body differs:
  - tdMode defaults to "cash" (on SPOT cross/isolated means spot margin
    trading, not used by default in the current SDK version).
  - NO posSide.
  - NO reduceOnly.
  - For market BUY, tgtCcy (base_ccy/quote_ccy) is available — see doc.
  - sz is measured in the BASE currency (or in quote_ccy for market BUY
    when tgtCcy="quote_ccy").

INVARIANTS:
  - clOrdId [A-Za-z0-9]{1,32} (same as SWAP — same OKX restriction).
  - Batch size — 20 (same as SWAP).
  - OKX has no global cancel-all-by-instrument; emulated via
    GetOpenOrders + CancelBatchOrders.

INTERNAL STATE:
  - clOrdToOrd/ordToClOrd: same ID mappings as in SWAP.

DEPENDENCIES:
  - internal/rest: transport.
  - spot/types:    SPOT domain structures.
  - github.com/tonymontanov/go-okx/v2: errors and rate-limit categories.
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

// MaxBatchSize — OKX limit for batch trade endpoints (same for SPOT and SWAP).
const MaxBatchSize = 20

// clOrdIDPattern — allowed characters and length for clOrdId.
// OKX: case-sensitive alphanumerics [A-Za-z0-9]{1,32}; '_', '-', '.' are rejected
// with code 51000.
var clOrdIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

// TradingClient — SPOT trading sub-client.
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

// orderActionResponseEntry — per-item result structure for all trade/*
// endpoints (OKX uses a single format).
type orderActionResponseEntry struct {
	OrdID   string `json:"ordId"`
	ClOrdID string `json:"clOrdId"`
	Tag     string `json:"tag"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

// uniqSortedInstIDsCreate — unique sorted set of InstIDs from a batch,
// required for RateLimitEvent.Symbols (the external rate-limiter models
// limits per (UID + InstId)).
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

// orderTypeFromTIF maps TIF to OKX OrderType. Identical to swap (same enum,
// see spot/types/aliases.go).
func orderTypeFromTIF(tif types.TimeInForceType) types.OrderType {
	switch tif {
	case types.TimeInForceTypeIOC:
		return types.OrderTypeIOC
	case types.TimeInForceTypeFOK:
		return types.OrderTypeFOK
	case types.TimeInForceTypeGTX:
		return types.OrderTypePostOnly
	case types.TimeInForceTypeRPI:
		return types.OrderTypeRPI
	case types.TimeInForceTypeGTC, "":
		return types.OrderTypeLimit
	default:
		return types.OrderTypeLimit
	}
}

/*
CreateOrder creates a SPOT order.

Parameters:
  - ctx: context with deadline.
  - req: order parameters. InstID/Side/Size are required; Price is required
    for all OrderTypes except market.

Returns OrderInfo with OrderID/ClientOrderID/CreatedAtMs/RateLimits populated.
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

// buildCreateOrderBody assembles map[string]any for SPOT order creation.
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
	// tgtCcy is only meaningful for market orders — OKX ignores it for limit,
	// but we omit it explicitly to avoid sending unnecessary fields.
	if orderType == types.OrderTypeMarket && req.TgtCcy != "" {
		body["tgtCcy"] = string(req.TgtCcy)
	}
	if req.Ccy != "" {
		body["ccy"] = req.Ccy
	}
	if req.Tag != "" {
		body["tag"] = req.Tag
	}
	// rpiTakerAccess is emitted only when true; absent-key keeps legacy
	// behaviour intact for accounts without RPI taker access.
	if req.RPITakerAccess {
		body["rpiTakerAccess"] = true
	}

	return body, nil
}

// buildAmendOrderBody — shared body builder for amend-order (single and batch
// variants, REST and WS). Validation is centralised here so that WS and REST
// return the same typed errors for the same inputs.
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
	// OKX does not inherit rpiTakerAccess on amend: the flag must be
	// re-specified on every amend request, otherwise the order silently
	// falls back to non-RPI matching.
	if req.RPITakerAccess {
		body["rpiTakerAccess"] = true
	}
	return body, nil
}

// buildCancelOrderBody — shared body builder for cancel-order (REST and WS).
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

// placeholderInfosModify — REST/WS-symmetric placeholder on transport-level
// error (chunk failed to send).
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
ModifyOrder — amend an order. OKX only changes sz/px; side/type cannot be changed.
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
CancelOrder cancels a single order. Exactly one identifier must be set.
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
CreateBatchOrders creates a batch of orders. Up to 20 per chunk. Return semantics
are the same as in swap: the position in the output slice matches the position in
the input; errors are aggregated via errors.Join.
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
ModifyBatchOrders — batch amend of orders (up to 20 per chunk).
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
		// rpiTakerAccess is not inherited on amend — re-emit per request.
		if r.RPITakerAccess {
			b["rpiTakerAccess"] = true
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
CancelBatchOrders cancels a batch of orders (up to 20 per chunk).
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
CancelAllOrders cancels ALL active orders for an instrument via
GetOpenOrders → CancelBatchOrders. Mirrors the SWAP implementation.
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
CancelForgottenOrders cancels orders older than maxAge.
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
CancelAllAfter arms (or disarms) the server-side dead-man's switch timer:
if the client does not call the endpoint again within `timeout` seconds,
OKX will automatically cancel ALL open orders for the account
(not just SPOT — all instruments).

PARAMETERS:
  - timeout > 0  (10..120s per OKX spec): arm. TriggerTimeMs in the response
    is the moment when the exchange will apply cancel-all if not refreshed.
  - timeout == 0: disarm. TriggerTimeMs in the response = 0.

HFT USAGE:
In the hot-loop strategy, call every ~⅓ of timeout (e.g. for timeout=30s —
every 10s). This provides a large buffer for network delays.

RELATION TO MASSCANCEL:
mass-cancel (WS, Phase 2.1) — synchronous panic button "cancel everything now".
cancel-all-after — asynchronous "cancel everything in N seconds if I do not
refresh". Used together: arm on start, mass-cancel or disarm on graceful shutdown.
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

// parseInt64Lossy parses an OKX timestamp string to int64; returns 0 on error.
// Used only for safe parsing of numeric strings where an error is non-critical
// (does not block the contract).
func parseInt64Lossy(s string) (int64, error) {
	var v int64
	var err error
	v, err = strconvAtoi64(s)
	return v, err
}

// strconvAtoi64 — extracted to avoid a direct strconv import in this file
// (file style: minimal imports; strconv is already imported in other package files).
func strconvAtoi64(s string) (int64, error) {
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

// rememberMapping adds ClOrdID ↔ OrdID and records the creation time.
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

// forgetMappingByClOrdOrOrd removes the mapping by either identifier.
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

// OrderIDByClientID returns OrdID by ClOrdID if it is known to the SDK.
func (t *TradingClient) OrderIDByClientID(clOrdID string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var v string
	var ok bool
	v, ok = t.clOrdToOrd[clOrdID]
	return v, ok
}

// ClientIDByOrderID returns ClOrdID by OrdID if it is known to the SDK.
func (t *TradingClient) ClientIDByOrderID(ordID string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var v string
	var ok bool
	v, ok = t.ordToClOrd[ordID]
	return v, ok
}

/*
SyncOrderMappings reloads mappings from open orders on the exchange.
Removes from the local map any orders not present in the exchange response.
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
