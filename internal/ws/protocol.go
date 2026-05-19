/*
FILE: internal/ws/protocol.go

DESCRIPTION:
OKX WebSocket v5 protocol message types: subscribe/unsubscribe/login
(control commands), channel push messages, and request/reply operations for
the WS Order API (op=order/cancel-order/amend-order/batch-orders/...).

FORMATS:

  Subscribe/unsubscribe (no correlation):
    { "op": "subscribe", "args": [ { "channel": "...", "instId": "..." } ] }

  Login:
    { "op": "login",
      "args": [ { "apiKey": "...", "passphrase": "...",
                  "timestamp": "...", "sign": "..." } ] }

  Event reply (for subscribe/unsubscribe/login/error):
    { "event": "subscribe"|"unsubscribe"|"login"|"error",
      "code": "0", "msg": "",
      "arg":  { "channel": "...", "instId": "..." },
      "connId": "..." }

  Push (channel data):
    { "arg":    { "channel": "...", "instId": "..." },
      "action": "snapshot"|"update",  // only for book channels
      "data":   [ ... ] }

  Ping / Pong: text frames "ping" / "pong" (NOT JSON).

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

REPLY VS PUSH:
A single read-loop over a single socket receives both push messages (no id
field) and op-command replies (always have id). Therefore the envelope in this
file is extended with optional fields id/op/inTime/outTime — the read-loop
dispatches based on the presence of id: if present — looks up the pending
request in the op-pending-map, otherwise handles as push/event.
*/

package ws

import (
	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
)

// subscribeArg — args element in subscribe/unsubscribe commands.
type subscribeArg struct {
	Channel  string `json:"channel"`
	InstID   string `json:"instId,omitempty"`
	InstType string `json:"instType,omitempty"`
}

// opRequest — subscribe/unsubscribe command of the form {op, args}.
type opRequest struct {
	Op   string         `json:"op"`
	Args []subscribeArg `json:"args"`
}

// loginArg — args element for login (op=login).
type loginArg struct {
	APIKey     string `json:"apiKey"`
	Passphrase string `json:"passphrase"`
	Timestamp  string `json:"timestamp"`
	Sign       string `json:"sign"`
}

// loginRequest — login command.
type loginRequest struct {
	Op   string     `json:"op"`
	Args []loginArg `json:"args"`
}

// incomingEnvelope — common envelope for incoming messages. A single type
// covers three OKX message forms:
//  1. push (has Arg.Channel and Data, no Event or ID);
//  2. event reply for subscribe/unsubscribe/login/error (has Event, no ID);
//  3. op-reply for WS Order API (has ID and Op, no Event).
//
// Data is kept as codec.RawMessage so that the handler of a specific channel
// or op-command can deserialize it into its typed destination without a second
// json.Unmarshal pass.
type incomingEnvelope struct {
	// Fields common / applicable to different message forms.
	Arg    pushArg          `json:"arg"`
	Action string           `json:"action,omitempty"`
	Data   codec.RawMessage `json:"data,omitempty"`
	Event  string           `json:"event,omitempty"`
	Code   string           `json:"code,omitempty"`
	Msg    string           `json:"msg,omitempty"`

	// op-reply fields: ID echoed from request, Op — command name,
	// InTime/OutTime — gateway timings (microseconds, as strings).
	ID      string `json:"id,omitempty"`
	Op      string `json:"op,omitempty"`
	InTime  string `json:"inTime,omitempty"`
	OutTime string `json:"outTime,omitempty"`
}

// pushEnvelope is kept as an alias for incomingEnvelope for backward
// compatibility with existing call-sites in conn.go.
type pushEnvelope = incomingEnvelope

// pushArg — arg field of a push message.
type pushArg struct {
	Channel  string `json:"channel"`
	InstID   string `json:"instId,omitempty"`
	InstType string `json:"instType,omitempty"`
}

// buildLogin builds the login command per the specification:
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
PUBLIC CONTRACT FOR WS Order API.

OpRequest and OpResponse are a neutral pair of types through which domain
layers (spot/swap/...) communicate with Conn to send op=order/cancel-order/
amend-order/batch-orders/cancel-batch-orders/amend-batch-orders/mass-cancel
commands.

ID:
The caller controls the correlation-id. Conn.SendOp generates a unique
monotonically-increasing id if the caller passed an empty one. Matching id
in the reply is the required condition for dispatch.

Args:
Passed as any and serialized into array [ ... ]. The domain layer must pass
a slice/array (e.g. []spottypes.CreateOrderRequest). This is intentional:
different ops have DIFFERENT args formats and imposing a common type here
would be premature generalization.

Data:
Left as codec.RawMessage in OpResponse for the same reasons as in
push-envelope: different ops return different structures (e.g. for "order"
it is an array of CreateOrderResponseItem with sCode/sMsg per entry).
The domain layer does the final Unmarshal into its typed destination.

Code/Msg:
Top-level WS frame code. "0" — command received successfully. code != "0"
means the command was rejected entirely (e.g. 60012 "Illegal request").
Per-item errors (sCode/sMsg inside Data) are handled by the domain layer.

InTime/OutTime:
Gateway timings (microseconds unix epoch as strings) — useful for
HFT latency diagnostics: (OutTime - InTime) gives time on the gateway side,
and (recv - OutTime) gives network RTT back to the client.
*/

// OpRequest — WS Order API request.
type OpRequest struct {
	// ID — client correlation-id. If empty, Conn.SendOp will generate one.
	ID string
	// Op — operation name (order/cancel-order/amend-order/batch-orders/
	// cancel-batch-orders/amend-batch-orders/mass-cancel).
	Op string
	// Args — payload array. Will be serialized as an array in the "args" field.
	// type any is intentional: each op-command has its own payload shape.
	Args any
}

// OpResponse — WS Order API reply.
type OpResponse struct {
	ID      string
	Op      string
	Code    string
	Msg     string
	Data    codec.RawMessage
	InTime  string
	OutTime string
}

// opRequestMessage — wire format for sending an OpRequest. Internal type,
// used only within the ws package.
type opRequestMessage struct {
	ID   string `json:"id,omitempty"`
	Op   string `json:"op"`
	Args any    `json:"args,omitempty"`
}
