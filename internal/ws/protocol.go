/*
ФАЙЛ: internal/ws/protocol.go

ОПИСАНИЕ:
Типы протокольных сообщений OKX WebSocket v5: subscribe/unsubscribe/login
(управляющие команды), push-сообщения каналов и request/reply-операции
для WS Order API (op=order/cancel-order/amend-order/batch-orders/...).

ФОРМАТЫ:

  Subscribe/unsubscribe (без correlation):
    { "op": "subscribe", "args": [ { "channel": "...", "instId": "..." } ] }

  Login:
    { "op": "login",
      "args": [ { "apiKey": "...", "passphrase": "...",
                  "timestamp": "...", "sign": "..." } ] }

  Event-ответ (на subscribe/unsubscribe/login/error):
    { "event": "subscribe"|"unsubscribe"|"login"|"error",
      "code": "0", "msg": "",
      "arg":  { "channel": "...", "instId": "..." },
      "connId": "..." }

  Push (данные канала):
    { "arg":    { "channel": "...", "instId": "..." },
      "action": "snapshot"|"update",  // только для книжных каналов
      "data":   [ ... ] }

  Ping / Pong: текстовые фреймы "ping" / "pong" (НЕ JSON).

  WS Order API request (op = order|cancel-order|amend-order|batch-orders|
                            cancel-batch-orders|amend-batch-orders|mass-cancel):
    { "id":   "<client-correlation-id>",
      "op":   "order",
      "args": [ { ... payload ... } ] }

  WS Order API reply:
    { "id":      "<echo of request id>",
      "op":      "order",
      "code":    "0",
      "msg":     "",
      "data":    [ { "ordId": "...", "clOrdId": "...",
                     "sCode": "0", "sMsg": "" } ],
      "inTime":  "<gateway in>",
      "outTime": "<gateway out>" }

ОТЛИЧИЕ REPLY ОТ PUSH:
В одном read-loop через один сокет приходят и push-сообщения (без поля id),
и reply на op-команды (всегда с id). Поэтому envelope в этом файле
расширен необязательными полями id/op/inTime/outTime — read-loop диспетчит
по факту наличия id: если есть — ищет pending-request в op-pending-map,
иначе обрабатывает как push/event.
*/

package ws

import (
	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
)

// subscribeArg — элемент args в командах subscribe/unsubscribe.
type subscribeArg struct {
	Channel  string `json:"channel"`
	InstID   string `json:"instId,omitempty"`
	InstType string `json:"instType,omitempty"`
}

// opRequest — команда subscribe/unsubscribe вида {op, args}.
type opRequest struct {
	Op   string         `json:"op"`
	Args []subscribeArg `json:"args"`
}

// loginArg — элемент args для логина (op=login).
type loginArg struct {
	APIKey     string `json:"apiKey"`
	Passphrase string `json:"passphrase"`
	Timestamp  string `json:"timestamp"`
	Sign       string `json:"sign"`
}

// loginRequest — команда логина.
type loginRequest struct {
	Op   string     `json:"op"`
	Args []loginArg `json:"args"`
}

// incomingEnvelope — общий envelope входящего сообщения. Один тип покрывает
// три формы сообщений OKX:
//  1. push (есть Arg.Channel и Data, нет Event и ID);
//  2. event-ответ на subscribe/unsubscribe/login/error (есть Event, нет ID);
//  3. op-reply на WS Order API (есть ID и Op, нет Event).
//
// Поле Data оставлено как codec.RawMessage, чтобы handler конкретного канала
// или op-команды десериализовал его в свой типизированный destination без
// второго прохода по json.Unmarshal.
type incomingEnvelope struct {
	// Поля, общие/применимые к разным формам.
	Arg    pushArg          `json:"arg"`
	Action string           `json:"action,omitempty"`
	Data   codec.RawMessage `json:"data,omitempty"`
	Event  string           `json:"event,omitempty"`
	Code   string           `json:"code,omitempty"`
	Msg    string           `json:"msg,omitempty"`

	// Поля op-reply: ID echoed-from-request, Op — имя команды,
	// InTime/OutTime — gateway timings (микросекунды, как строки).
	ID      string `json:"id,omitempty"`
	Op      string `json:"op,omitempty"`
	InTime  string `json:"inTime,omitempty"`
	OutTime string `json:"outTime,omitempty"`
}

// pushEnvelope сохранён как алиас на incomingEnvelope для обратной
// совместимости с существующими call-sites в conn.go.
type pushEnvelope = incomingEnvelope

// pushArg — поле arg push-сообщения.
type pushArg struct {
	Channel  string `json:"channel"`
	InstID   string `json:"instId,omitempty"`
	InstType string `json:"instType,omitempty"`
}

// buildLogin строит команду логина согласно спецификации:
//
//	preHash = timestamp + "GET" + "/users/self/verify"
//	sign    = base64(HMAC_SHA256(secret, preHash))
func buildLogin(signer *auth.Signer, timestamp string) (loginRequest, error) {
	var sig string
	var err error
	sig, err = signer.Sign(timestamp, "GET", "/users/self/verify", "")
	if err != nil {
		return loginRequest{}, err
	}
	return loginRequest{
		Op: "login",
		Args: []loginArg{{
			APIKey:     signer.APIKey(),
			Passphrase: signer.Passphrase(),
			Timestamp:  timestamp,
			Sign:       sig,
		}},
	}, nil
}

/*
ПУБЛИЧНЫЙ КОНТРАКТ ДЛЯ WS Order API.

OpRequest и OpResponse — это нейтральная пара типов, через которую доменные
слои (spot/swap/...) общаются с Conn для отправки команд op=order/cancel-
order/amend-order/batch-orders/cancel-batch-orders/amend-batch-orders/mass-
cancel.

ID:
Клиент сам контролирует correlation-id. Conn.SendOp генерирует уникальный
монотонно-возрастающий id, если вызывающий передал пустой. Совпадение id
в ответе — обязательное условие диспатча.

Args:
Передаётся как any и сериализуется в массив [ ... ]. Доменный слой
обязан передавать срез/массив (например []spottypes.CreateOrderRequest).
Это сделано осознанно: разные op'ы имеют РАЗНЫЕ форматы args, и навязывать
здесь общий тип было бы преждевременной обобщённостью.

Data:
В OpResponse оставлено как codec.RawMessage по тем же причинам, что и в
push-envelope: разные op'ы возвращают разные структуры (например для
"order" это массив CreateOrderResponseItem с sCode/sMsg по каждой заявке).
Доменный слой делает финальный Unmarshal в свой типизированный destination.

Code/Msg:
Top-level код WS-фрейма. "0" — успешный приём команды. При code != "0"
команда отвергнута целиком (например 60012 "Illegal request"). Per-item
ошибки (sCode/sMsg внутри Data) разбирает доменный слой.

InTime/OutTime:
Gateway timings (микросекунды unix epoch как строки) — полезны для
HFT-латентностной диагностики: (OutTime - InTime) даёт время на стороне
gateway, а (recv - OutTime) — network RTT обратно к клиенту.
*/

// OpRequest — запрос для WS Order API.
type OpRequest struct {
	// ID — клиентский correlation-id. Если пустой, Conn.SendOp сгенерирует.
	ID string
	// Op — имя операции (order/cancel-order/amend-order/batch-orders/
	// cancel-batch-orders/amend-batch-orders/mass-cancel).
	Op string
	// Args — payload-массив. Будет сериализован как массив в поле "args".
	// Тип Any выбран осознанно: каждая op-команда имеет свой payload-shape.
	Args any
}

// OpResponse — ответ на WS Order API.
type OpResponse struct {
	ID      string
	Op      string
	Code    string
	Msg     string
	Data    codec.RawMessage
	InTime  string
	OutTime string
}

// opRequestMessage — wire-формат для отправки OpRequest. Внутренний тип,
// используется только внутри пакета ws.
type opRequestMessage struct {
	ID   string `json:"id,omitempty"`
	Op   string `json:"op"`
	Args any    `json:"args,omitempty"`
}
