/*
FILE: swap/trading_ws.go

DESCRIPTION:
WSTradingClient — trading sub-client via the WebSocket Order API for SWAP.
Exposes the same surface as REST-TradingClient (CreateOrder/ModifyOrder/
CancelOrder + batch + MassCancel), but sends commands over a single
established private-WS conn instead of an HTTP/2 round-trip.

LATENCY GAIN vs REST:
Typically 30-50% time-to-exchange: TLS handshake / HTTP framing / TCP slow-start
are eliminated; only TLS encrypt + write on the already-established socket +
read reply remain. For perp MM this is the difference between getting into
the queue before toxic flow and not.

MULTIPLEXING:
The WS Order API lives on THE SAME private-WS conn as subscriptions
(orders/positions/account). SendOp in internal/ws/conn.go correctly
correlates replies by id without confusing them with push messages.

PAYLOAD:
buildCreateOrderBody / buildAmendOrderBody / buildCancelOrderBody from
trading.go are reused — no duplicated validation logic.

RATE-LIMITS:
The OKX WS Order API has SEPARATE limits (4000 orders/s total).
They are NOT modelled in the SDK (observers are only wired to the REST
transport). This is intentional: WS limits rarely become a bottleneck,
and invasive integration into the WS loop would require a dedicated observer API.

MASS-CANCEL:
Fully applicable to SWAP: instType="SWAP", instFamily=<base-quote>
(e.g. "BTC-USDT") cancels all orders on perps of that underlying in
a single sweep. This is a panic-button for risk management.
*/

package swap

import (
	"context"
	"errors"
	"fmt"
	"time"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/ws"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// WSDefaultTimeout — soft ceiling for reply wait when no ctx-deadline is set.
// 10 seconds is well above the real WS RTT and protects against hangs on
// reply-pipeline failures.
const WSDefaultTimeout = 10 * time.Second

// WSTradingClient — sub-client WS Order API.
type WSTradingClient struct {
	t *TradingClient
}

// WS returns the WS trading variant. Idempotent: one instance per TradingClient.
// Lazy: actual connect/login happens on the first WS method call.
func (t *TradingClient) WS() *WSTradingClient {
	return &WSTradingClient{t: t}
}

func (w *WSTradingClient) conn() *ws.Conn {
	return w.t.c.privateConn()
}

func (w *WSTradingClient) ensureReady(ctx context.Context) error {
	if !w.t.c.signerEnabled() {
		return okx.NewError(okx.ErrorKindAuth, "", "ws trading: signer disabled (api credentials required)", nil)
	}
	return w.conn().EnsureReady(ctx)
}

/*
CreateOrder — WS variant of single perp order creation. Signature identical to
REST: switch by replacing .Trading() with .Trading().WS().
*/
func (w *WSTradingClient) CreateOrder(ctx context.Context, req types.CreateOrderRequest) (types.OrderInfo, error) {
	var info types.OrderInfo
	var err error

	var body map[string]any
	body, err = w.t.buildCreateOrderBody(req)
	if err != nil {
		return info, err
	}
	if err = w.ensureReady(ctx); err != nil {
		return info, err
	}

	var entries []orderActionResponseEntry
	entries, err = w.doOp(ctx, "order", []any{body})
	if err != nil {
		return info, err
	}
	if len(entries) == 0 {
		return info, okx.NewError(okx.ErrorKindExchange, "", "ws trading.CreateOrder: empty data", nil)
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
	}
	w.t.rememberMapping(e.ClOrdID, e.OrdID, info.CreatedAtMs)
	return info, nil
}

// ModifyOrder — WS variant of amend.
func (w *WSTradingClient) ModifyOrder(ctx context.Context, req types.ModifyOrderRequest) (types.OrderInfo, error) {
	var info types.OrderInfo
	var err error

	var body map[string]any
	body, err = buildAmendOrderBody(req)
	if err != nil {
		return info, err
	}
	if err = w.ensureReady(ctx); err != nil {
		return info, err
	}

	var entries []orderActionResponseEntry
	entries, err = w.doOp(ctx, "amend-order", []any{body})
	if err != nil {
		return info, err
	}
	if len(entries) == 0 {
		return info, okx.NewError(okx.ErrorKindExchange, "", "ws trading.ModifyOrder: empty data", nil)
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
	}
	return info, nil
}

// CancelOrder — WS variant of single order cancel.
func (w *WSTradingClient) CancelOrder(ctx context.Context, req types.CancelOrderRequest) error {
	var body map[string]any
	var err error
	body, err = buildCancelOrderBody(req)
	if err != nil {
		return err
	}
	if err = w.ensureReady(ctx); err != nil {
		return err
	}

	var entries []orderActionResponseEntry
	entries, err = w.doOp(ctx, "cancel-order", []any{body})
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return okx.NewError(okx.ErrorKindExchange, "", "ws trading.CancelOrder: empty data", nil)
	}
	var e orderActionResponseEntry = entries[0]
	if e.SCode != "" && e.SCode != "0" {
		return &okx.Error{
			Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
			OKXCode: e.SCode,
			Message: e.SMsg,
		}
	}
	w.t.forgetMappingByClOrdOrOrd(req.ClientOrderID, req.OrderID)
	return nil
}

// CreateBatchOrders — WS variant of batch create.
func (w *WSTradingClient) CreateBatchOrders(ctx context.Context, reqs []types.CreateOrderRequest) ([]types.OrderInfo, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	if err := w.ensureReady(ctx); err != nil {
		return nil, err
	}

	var out []types.OrderInfo = make([]types.OrderInfo, 0, len(reqs))
	var aggErrs []error
	var chunkStart int
	for chunkStart = 0; chunkStart < len(reqs); chunkStart += MaxBatchSize {
		var chunkEnd int = chunkStart + MaxBatchSize
		if chunkEnd > len(reqs) {
			chunkEnd = len(reqs)
		}
		var chunk []types.CreateOrderRequest = reqs[chunkStart:chunkEnd]
		var infos []types.OrderInfo
		var err error
		infos, err = w.createBatchChunkWS(ctx, chunk)
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

func (w *WSTradingClient) createBatchChunkWS(ctx context.Context, chunk []types.CreateOrderRequest) ([]types.OrderInfo, error) {
	var bodies []any = make([]any, 0, len(chunk))
	var bodyErrs []error
	var err error
	var i int
	for i = 0; i < len(chunk); i++ {
		var b map[string]any
		b, err = w.t.buildCreateOrderBody(chunk[i])
		if err != nil {
			bodyErrs = append(bodyErrs, fmt.Errorf("batch[%d]: %w", i, err))
			continue
		}
		bodies = append(bodies, b)
	}
	if len(bodies) == 0 {
		return placeholderInfos(chunk), errors.Join(bodyErrs...)
	}

	var entries []orderActionResponseEntry
	entries, err = w.doOp(ctx, "batch-orders", bodies)
	if err != nil {
		return placeholderInfos(chunk), err
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
		}
		if e.SCode != "" && e.SCode != "0" {
			info.State = types.OrderStateUnknown
			aggErrs = append(aggErrs, &okx.Error{
				Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
				OKXCode: e.SCode,
				Message: e.SMsg,
			})
		} else {
			w.t.rememberMapping(e.ClOrdID, e.OrdID, now)
		}
		infos = append(infos, info)
	}
	if len(aggErrs) > 0 {
		return infos, errors.Join(aggErrs...)
	}
	return infos, nil
}

// ModifyBatchOrders — WS variant of amend-batch.
func (w *WSTradingClient) ModifyBatchOrders(ctx context.Context, reqs []types.ModifyOrderRequest) ([]types.OrderInfo, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	if err := w.ensureReady(ctx); err != nil {
		return nil, err
	}

	var out []types.OrderInfo = make([]types.OrderInfo, 0, len(reqs))
	var aggErrs []error
	var chunkStart int
	for chunkStart = 0; chunkStart < len(reqs); chunkStart += MaxBatchSize {
		var chunkEnd int = chunkStart + MaxBatchSize
		if chunkEnd > len(reqs) {
			chunkEnd = len(reqs)
		}
		var infos []types.OrderInfo
		var err error
		infos, err = w.modifyBatchChunkWS(ctx, reqs[chunkStart:chunkEnd])
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

func (w *WSTradingClient) modifyBatchChunkWS(ctx context.Context, chunk []types.ModifyOrderRequest) ([]types.OrderInfo, error) {
	var bodies []any = make([]any, 0, len(chunk))
	var bodyErrs []error
	var i int
	for i = 0; i < len(chunk); i++ {
		var b map[string]any
		var err error
		b, err = buildAmendOrderBody(chunk[i])
		if err != nil {
			bodyErrs = append(bodyErrs, fmt.Errorf("batch[%d]: %w", i, err))
			continue
		}
		bodies = append(bodies, b)
	}
	if len(bodies) == 0 {
		return placeholderInfosModify(chunk), errors.Join(bodyErrs...)
	}

	var entries []orderActionResponseEntry
	var err error
	entries, err = w.doOp(ctx, "amend-batch-orders", bodies)
	if err != nil {
		return placeholderInfosModify(chunk), err
	}

	var infos []types.OrderInfo = make([]types.OrderInfo, 0, len(chunk))
	var aggErrs []error = bodyErrs
	var now int64 = time.Now().UnixMilli()
	for i = 0; i < len(chunk); i++ {
		if i >= len(entries) {
			infos = append(infos, types.OrderInfo{
				InstID:        chunk[i].InstID,
				ClientOrderID: chunk[i].ClientOrderID,
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

// CancelBatchOrders — WS variant of cancel-batch.
func (w *WSTradingClient) CancelBatchOrders(ctx context.Context, reqs []types.CancelOrderRequest) error {
	if len(reqs) == 0 {
		return nil
	}
	if err := w.ensureReady(ctx); err != nil {
		return err
	}

	var aggErrs []error
	var chunkStart int
	for chunkStart = 0; chunkStart < len(reqs); chunkStart += MaxBatchSize {
		var chunkEnd int = chunkStart + MaxBatchSize
		if chunkEnd > len(reqs) {
			chunkEnd = len(reqs)
		}
		var err error = w.cancelBatchChunkWS(ctx, reqs[chunkStart:chunkEnd])
		if err != nil {
			aggErrs = append(aggErrs, err)
		}
	}
	if len(aggErrs) > 0 {
		return errors.Join(aggErrs...)
	}
	return nil
}

func (w *WSTradingClient) cancelBatchChunkWS(ctx context.Context, chunk []types.CancelOrderRequest) error {
	var bodies []any = make([]any, 0, len(chunk))
	var bodyErrs []error
	var i int
	for i = 0; i < len(chunk); i++ {
		var b map[string]any
		var err error
		b, err = buildCancelOrderBody(chunk[i])
		if err != nil {
			bodyErrs = append(bodyErrs, fmt.Errorf("batch[%d]: %w", i, err))
			continue
		}
		bodies = append(bodies, b)
	}
	if len(bodies) == 0 {
		return errors.Join(bodyErrs...)
	}

	var entries []orderActionResponseEntry
	var err error
	entries, err = w.doOp(ctx, "cancel-batch-orders", bodies)
	if err != nil {
		return err
	}

	var aggErrs []error = bodyErrs
	for i = 0; i < len(entries); i++ {
		var e orderActionResponseEntry = entries[i]
		if e.SCode != "" && e.SCode != "0" {
			aggErrs = append(aggErrs, &okx.Error{
				Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
				OKXCode: e.SCode,
				Message: e.SMsg,
			})
			continue
		}
		w.t.forgetMappingByClOrdOrOrd(e.ClOrdID, e.OrdID)
	}
	if len(aggErrs) > 0 {
		return errors.Join(aggErrs...)
	}
	return nil
}

/*
MassCancel — bulk cancel by group. For SWAP the parameter instType="SWAP",
instFamily is the underlying pair, e.g. "BTC-USDT". ALL open orders on perps
of that underlying are cancelled in a single operation.

PANIC-BUTTON:
Used as a safety mechanism on price-source loss, feed disconnect, or emergency
strategy shutdown. Completes in one WS RTT.

RELATION TO CANCELALLAFTER:
mass-cancel — synchronous, cancels NOW.
cancel-all-after (REST, Phase 2.2) — arm timer: "cancel on your own if I don't
send a signal within N seconds". Used together: arm at startup, mass-cancel on
shutdown.
*/
func (w *WSTradingClient) MassCancel(ctx context.Context, instType, instFamily string) error {
	if instType == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "ws trading.MassCancel: instType is empty", nil)
	}
	if instFamily == "" {
		return okx.NewError(okx.ErrorKindInvalidRequest, "", "ws trading.MassCancel: instFamily is empty", nil)
	}
	if err := w.ensureReady(ctx); err != nil {
		return err
	}
	var body map[string]any = map[string]any{
		"instType":   instType,
		"instFamily": instFamily,
	}
	var entries []orderActionResponseEntry
	var err error
	entries, err = w.doOp(ctx, "mass-cancel", []any{body})
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	var e orderActionResponseEntry = entries[0]
	if e.SCode != "" && e.SCode != "0" {
		return &okx.Error{
			Kind:    okx.MapOKXCode(e.SCode, e.SMsg),
			OKXCode: e.SCode,
			Message: e.SMsg,
		}
	}
	return nil
}

/*
CancelAllAfter — WS variant of dead-man's switch (op="cancel-all-after").
Semantics identical to the REST variant in trading.go: timeout > 0 — arm
(10..120s per OKX spec), timeout == 0 — disarm. WS advantage — lower latency
for the strategy's refresh loop.
*/
func (w *WSTradingClient) CancelAllAfter(ctx context.Context, timeout time.Duration) (types.CancelAllAfterResult, error) {
	var out types.CancelAllAfterResult
	if timeout < 0 {
		return out, okx.NewError(okx.ErrorKindInvalidRequest, "", "ws trading.CancelAllAfter: timeout must be >= 0", nil)
	}
	if err := w.ensureReady(ctx); err != nil {
		return out, err
	}
	var seconds int64 = int64(timeout / time.Second)
	var body map[string]any = map[string]any{
		"timeOut": fmt.Sprintf("%d", seconds),
	}

	var resp ws.OpResponse
	var err error
	resp, err = w.conn().SendOp(ctx, ws.OpRequest{
		Op:   "cancel-all-after",
		Args: []any{body},
	}, WSDefaultTimeout)
	if err != nil {
		return out, err
	}
	if resp.Code == "disconnected" {
		return out, okx.NewError(okx.ErrorKindNetwork, "", "ws trading.CancelAllAfter: connection lost", nil)
	}
	if resp.Code != "" && resp.Code != "0" {
		return out, &okx.Error{
			Kind:    okx.MapOKXCode(resp.Code, resp.Msg),
			OKXCode: resp.Code,
			Message: "ws trading.CancelAllAfter: " + resp.Msg,
		}
	}
	if len(resp.Data) == 0 {
		return out, nil
	}

	type rawEntry struct {
		TriggerTime string `json:"triggerTime"`
		Ts          string `json:"ts"`
	}
	var entries []rawEntry
	if err = codec.Unmarshal(resp.Data, &entries); err != nil {
		return out, okx.NewError(okx.ErrorKindUnknown, "", "ws trading.CancelAllAfter: parse", err)
	}
	if len(entries) == 0 {
		return out, nil
	}
	if entries[0].TriggerTime != "" {
		out.TriggerTimeMs, _ = parseInt64Lossy(entries[0].TriggerTime)
	}
	if entries[0].Ts != "" {
		out.TsMs, _ = parseInt64Lossy(entries[0].Ts)
	}
	return out, nil
}

/*
doOp — common wrapper over ws.Conn.SendOp: builds the OpRequest, waits for
the reply, checks top-level code, parses Data into []orderActionResponseEntry.
Disconnect-reply (Code="disconnected" from failAllPending) is converted to
ErrorKindNetwork — the caller can use errors.As/Is without string parsing.
*/
func (w *WSTradingClient) doOp(ctx context.Context, op string, args []any) ([]orderActionResponseEntry, error) {
	var resp ws.OpResponse
	var err error
	resp, err = w.conn().SendOp(ctx, ws.OpRequest{
		Op:   op,
		Args: args,
	}, WSDefaultTimeout)
	if err != nil {
		return nil, err
	}
	if resp.Code == "disconnected" {
		return nil, okx.NewError(okx.ErrorKindNetwork, "", "ws trading "+op+": connection lost", nil)
	}
	if resp.Code != "" && resp.Code != "0" {
		return nil, &okx.Error{
			Kind:    okx.MapOKXCode(resp.Code, resp.Msg),
			OKXCode: resp.Code,
			Message: "ws trading " + op + ": " + resp.Msg,
		}
	}
	if len(resp.Data) == 0 {
		return nil, nil
	}
	var entries []orderActionResponseEntry
	if err = codec.Unmarshal(resp.Data, &entries); err != nil {
		return nil, okx.NewError(okx.ErrorKindUnknown, "", "ws trading "+op+": parse data", err)
	}
	return entries, nil
}
