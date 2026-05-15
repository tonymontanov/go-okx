/*
ФАЙЛ: internal/ws/protocol.go

ОПИСАНИЕ:
Типы протокольных сообщений OKX WebSocket v5 и фабрики команд (subscribe,
unsubscribe, login).

ФОРМАТЫ:
- Команда: { "op": "...", "args": [ ... ] }
- Ответ-событие: { "event": "subscribe"|"unsubscribe"|"login"|"error",
                   "code": "0", "msg": "", "arg": { "channel": "...", "instId": "..." },
                   "connId": "..." }
- Push: { "arg": { "channel": "...", "instId": "..." },
          "action": "snapshot"|"update",     // только для каналов books
          "data": [ ... ] }
- Ping/Pong: текстовые фреймы "ping" / "pong" (НЕ JSON).
*/

package ws

import (
	"github.com/tonymontanov/go-okx/v2/internal/auth"
	"github.com/tonymontanov/go-okx/v2/internal/codec"
)

// subscribeArg — элемент args в командах subscribe/unsubscribe.
type subscribeArg struct {
	Channel string `json:"channel"`
	InstID  string `json:"instId,omitempty"`
	InstType string `json:"instType,omitempty"`
}

// opRequest — команда вида {op, args}.
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

// pushEnvelope — общий envelope входящего push'а. Поле Data оставлено как
// «сырое», чтобы handler конкретного канала десериализовал его в свой
// типизированный destination без второго прохода по json.Unmarshal.
type pushEnvelope struct {
	Arg    pushArg          `json:"arg"`
	Action string           `json:"action,omitempty"`
	Data   codec.RawMessage `json:"data,omitempty"`
	Event  string           `json:"event,omitempty"`
	Code   string           `json:"code,omitempty"`
	Msg    string           `json:"msg,omitempty"`
}

// pushArg — поле arg push-сообщения.
type pushArg struct {
	Channel string `json:"channel"`
	InstID  string `json:"instId,omitempty"`
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
