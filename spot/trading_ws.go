/*
FILE: spot/trading_ws.go

DESCRIPTION:
WSTradingClient — sub-client for trading via the WebSocket Order API. Exposes
the same set of operations as the REST variant (CreateOrder/ModifyOrder/
CancelOrder + batches + MassCancel), but sends them over an already established
private-WS connection instead of an HTTP/2 round-trip.

WHY:
Latency gain vs REST on OKX (typical) — 30-50% time-to-exchange:
TLS handshake / HTTP framing / TCP slow-start are eliminated; only
TLS encrypt + write on an established socket + read reply remain.
For HFT MM this is the difference between getting into the queue before
toxic flow and not.

IMPORTANT:
  - WS Order API channels live ON THE SAME private-WS conn as subscriptions
    (orders/account/positions). A single multiplexed socket is used; SendOp
    correctly correlates replies by id (see internal/ws/conn.go).
  - Payload body structure is identical to REST: reuses
    buildCreateOrderBody/buildModifyOrderBody/buildCancelOrderBody from
    trading.go to avoid duplicating validation logic.
  - Per-item OKX replies use the same orderActionResponseEntry (ordId/clOrdId/
    tag/sCode/sMsg) — parsed with the same code.

TIMEOUTS:
  - The default SendOp timeout is not hardcoded: ctx.Deadline() is used as
    the primary, plus a soft "sanity" 10-second timeout via SendOp(.., 10s)
    as protection against hangs when the reply pipeline fails.

RATE LIMITS:
  OKX WS Order API has SEPARATE limits (significantly higher than REST,
  4000 orders/s across all WS connections). These limits are NOT currently
  modelled in the SDK (rate-limit observers are connected to REST transport
  in internal/rest only). This is intentional: WS limits rarely become a
  bottleneck, and invasive integration into the WS loop would require a
  separate observer API. A user who actually hits WS limits can maintain
  their own counter on top of the SDK.

ID:
A per-command correlation id is generated automatically via SendOp.
clOrdId/tag are validated by the same rules as in REST.

NOTE ON ENSUREREADY:
SendOp requires the socket to already be established. To prevent the caller
from receiving ErrConnNotReady on the first call, WSTradingClient calls
EnsureReady automatically — this is idempotent and blocks until connect+login.
*/

package spot

import (
	"context"
	"errors"
	"fmt"
	"time"

	okx "github.com/tonymontanov/go-okx/v2"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
	"github.com/tonymontanov/go-okx/v2/internal/ws"
	"github.com/tonymontanov/go-okx/v2/spot/types"
)

// WSDefaultTimeout — soft ceiling for waiting on a reply when no ctx-deadline
// is set. 10 seconds is well above the real WS round-trip (tens of milliseconds);
// guards against hangs when the reply pipeline fails on the exchange side.
const WSDefaultTimeout = 10 * time.Second

// WSTradingClient — WS Order API sub-client. Returned via TradingClient.WS().
type WSTradingClient struct {
	t *TradingClient
}

// WS returns the WS trading variant. Idempotent: one instance per TradingClient.
// Lazy: nothing is connected on the first call; actual connect/login happens on
// the first WS method invocation.
func (t *TradingClient) WS() *WSTradingClient {
	return &WSTradingClient{t: t}
}

// conn — lazily starts the parent client's private WS connection.
func (w *WSTradingClient) conn() *ws.Conn {
	return w.t.c.privateConn()
}

// ensureReady guarantees that the private WS connection is started and the
// socket is established (login completed). Uses the caller's ctx for timeout.
func (w *WSTradingClient) ensureReady(ctx context.Context) error {
	if !w.t.c.signerEnabled() {
		return okx.NewError(okx.ErrorKindAuth, "", "ws trading: signer disabled (api credentials required)", nil)
	}
	return w.conn().EnsureReady(ctx)
}

/*
CreateOrder — WS variant of single-order creation. Signature is identical to
the REST variant: the caller can switch by replacing .Trading() with
.Trading().WS() without changing the call body.
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

/*
ModifyOrder — WS variant of amend.
*/
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

/*
CancelOrder — WS variant of single-order cancel.
*/
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

/*
CreateBatchOrders — WS variant of batch create. Up to MaxBatchSize per single
op-command; if exceeded, automatically splits into multiple sequential ops
(same as the REST variant).
*/
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

/*
ModifyBatchOrders — WS variant of amend-batch (up to MaxBatchSize per op).
*/
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

/*
CancelBatchOrders — WS variant of cancel-batch.
*/
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
MassCancel — bulk order cancellation by group. OKX op = "mass-cancel"
accepts instType + instFamily and cancels ALL open orders across all
instruments in the specified family. Applicable for SWAP/FUTURES/OPTION;
for SPOT instFamily is not defined and the operation does not make sense.

WARNING:
MassCancel is a destructive batch operation. Unlike CancelBatchOrders there
is no per-item control over what exactly is cancelled. Use only as a panic
button (e.g. on price/feed loss).

NOTE:
The method is available in the spot.WSTradingClient API for symmetry (one
sub-client shape for both profiles), but on SPOT the exchange returns 51400 —
instFamily is absent. Use swap.WSTradingClient.MassCancel instead.
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
CancelAllAfter — WS variant of the dead-man's switch (op="cancel-all-after").
Semantics are identical to the REST variant in trading.go: timeout > 0 — arm
(10..120s per OKX spec), timeout == 0 — disarm. WS advantage is lower latency
for the strategy's refresh cycle.
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
		out.TriggerTimeMs, _ = strconvAtoi64(entries[0].TriggerTime)
	}
	if entries[0].Ts != "" {
		out.TsMs, _ = strconvAtoi64(entries[0].Ts)
	}
	return out, nil
}

/*
doOp — common wrapper over SendOp: builds the OpRequest, waits for the reply,
checks the top-level code, and parses Data into an orderActionResponseEntry slice.

NOTE ON DISCONNECT:
If the connection dropped during SendOp, failAllPending in conn.go delivers
a synthetic reply with Code="disconnected". Here we convert it to a typed
error (Kind=Network) so the caller can use errors.Is/As without parsing
code strings.
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
