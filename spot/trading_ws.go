/*
ФАЙЛ: spot/trading_ws.go

ОПИСАНИЕ:
WSTradingClient — sub-client торговли через WebSocket Order API. Экспозит
тот же набор операций, что и REST-вариант (CreateOrder/ModifyOrder/
CancelOrder + батчи + MassCancel), но шлёт их через одно установленное
private-WS соединение вместо HTTP/2 round-trip'а.

ЗАЧЕМ:
Выигрыш в латентности vs REST на OKX (типично) — 30-50% time-to-exchange:
снимается TLS handshake / HTTP framing / TCP slow-start, остаётся
только TLS encrypt + write на уже установленный сокет + read reply.
Для HFT MM это разница между «успели в очередь до toxic-flow» и нет.

ВАЖНО:
  - WS-каналы Order API живут НА ТОМ ЖЕ private-WS conn, что и
    subscriptions (orders/account/positions). Используется один
    мультиплексированный socket; SendOp корректно correlate'ит reply
    по id (см. internal/ws/conn.go).
  - Payload-структура bodies полностью совпадает с REST: переиспользуем
    buildCreateOrderBody/buildModifyOrderBody/buildCancelOrderBody из
    trading.go, чтобы не плодить дублирующую логику валидации.
  - Per-item reply OKX тот же orderActionResponseEntry (ordId/clOrdId/
    tag/sCode/sMsg) — разбираем тем же кодом.

ТАЙМАУТЫ:
  - Дефолтный SendOp-таймаут не задан жёстко: используем ctx.Deadline()
    как primary, плюс мягкий «sanity» 10 секунд через SendOp(.., 10s)
    как защиту от подвисов при сбоях reply-pipeline.

RATE-LIMITS:
  WS Order API у OKX имеет ОТДЕЛЬНЫЕ лимиты (значительно выше REST,
  4000 orders/s суммарно для всех WS connections). Эти лимиты сейчас НЕ
  моделируются в SDK (rate-limit observers подключены только к REST-
  транспорту в internal/rest). Это сознательно: WS-лимиты редко
  становятся узким местом, а инвазивная интеграция в WS-loop потребует
  отдельной observer-API. Пользователь, который реально упирается в WS-
  лимит, может вести свой счётчик поверх SDK.

ID:
Per-команда генерируется свой correlation-id через SendOp (auto).
clOrdId/tag валидируются теми же правилами, что и в REST.

ПРИМЕЧАНИЕ О ENSUREREADY:
SendOp требует, чтобы сокет уже был установлен. Чтобы caller не получал
ErrConnNotReady на первом вызове, WSTradingClient вызывает EnsureReady
автоматически — это идемпотентно и блокируется до connect+login.
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

// WSDefaultTimeout — мягкий потолок на ожидание reply, если ctx-deadline
// не выставлен. 10 секунд — заведомо больше реального WS round-trip
// (десятки миллисекунд); защищает от зависаний при некорректном reply
// pipeline на стороне биржи.
const WSDefaultTimeout = 10 * time.Second

// WSTradingClient — sub-client WS Order API. Возвращается через
// TradingClient.WS().
type WSTradingClient struct {
	t *TradingClient
}

// WS возвращает WS-вариант торговли. Идемпотентно: один экземпляр на
// TradingClient. Lazy: при первом вызове ничего не подключает; реальный
// connect/login происходит при первом WS-методе.
func (t *TradingClient) WS() *WSTradingClient {
	return &WSTradingClient{t: t}
}

// conn — лениво поднимает private WS-conn родительского клиента.
func (w *WSTradingClient) conn() *ws.Conn {
	return w.t.c.privateConn()
}

// ensureReady гарантирует, что private WS-conn запущен и socket
// установлен (login пройден). Использует ctx caller'а для таймаута.
func (w *WSTradingClient) ensureReady(ctx context.Context) error {
	if !w.t.c.signerEnabled() {
		return okx.NewError(okx.ErrorKindAuth, "", "ws trading: signer disabled (api credentials required)", nil)
	}
	return w.conn().EnsureReady(ctx)
}

/*
CreateOrder — WS-вариант создания одного ордера. Сигнатура идентична
REST-варианту: пользователь может переключиться сменой .Trading() на
.Trading().WS() без изменения тела вызова.
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
ModifyOrder — WS-вариант amend.
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
CancelOrder — WS-вариант cancel одного ордера.
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
CreateBatchOrders — WS-вариант batch create. До MaxBatchSize за одну
op-команду; при превышении автоматически разбивает на несколько
последовательных op'ов (как REST-вариант).
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
ModifyBatchOrders — WS-вариант amend-batch (до MaxBatchSize за op).
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
CancelBatchOrders — WS-вариант cancel-batch.
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
MassCancel — массовая отмена ордеров по группе. OKX op = "mass-cancel"
принимает instType + instFamily; отменяет ВСЕ открытые ордера на всех
инструментах указанной семьи. Применимо для SWAP/FUTURES/OPTION; для
SPOT instFamily не определён и операция не имеет смысла.

ПРЕДУПРЕЖДЕНИЕ:
MassCancel — destructive batch операция. В отличие от CancelBatchOrders
здесь нет per-item контроля, чем именно отменять. Используйте только
как panic-button (например при потере цены/feeda).

ПРИМЕЧАНИЕ:
Метод доступен в spot.WSTradingClient API для симметрии (один shape sub-
client на оба профиля), но на SPOT возвращает 51400 от биржи —
instFamily нет. Используйте swap.WSTradingClient.MassCancel.
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
doOp — общая обёртка над SendOp: формирует OpRequest, ждёт reply,
проверяет top-level code, парсит Data в массив orderActionResponseEntry.

ВНИМАНИЕ ОБ DISCONNECT'Е:
Если соединение оборвалось во время SendOp, failAllPending в conn.go
доставляет синтетический reply с Code="disconnected". Здесь мы
конвертируем его в типизированную ошибку (Kind=Network), чтобы caller
мог делать errors.Is/As и не вязнуть в парсинге code-строк.
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
