/*
ФАЙЛ: internal/auth/sign.go

ОПИСАНИЕ:
Файл sign.go реализует подпись REST/WS-запросов к OKX v5. Алгоритм согласно
официальной документации:

  preHash = timestamp + method + requestPath + body
  signature = base64( HMAC_SHA256(secretKey, preHash) )

Где:
  - timestamp     — ISO8601 в UTC с миллисекундами и суффиксом 'Z',
                    напр. "2026-05-15T16:18:00.123Z".
  - method        — UPPER-CASE HTTP-метод ("GET" / "POST").
  - requestPath   — URL.Path плюс canonicalized query string (с '?'),
                    напр. "/api/v5/trade/order" или
                    "/api/v5/market/books?instId=BTC-USDT-SWAP&sz=20".
  - body          — тело JSON для POST / пустая строка для GET.

Те же 4 заголовка OK-ACCESS-KEY / SIGN / TIMESTAMP / PASSPHRASE отправляются
по подписанным REST-запросам и аналогично — в payload login-сообщения WS.

ОСНОВНЫЕ ФУНКЦИИ:
  - NewSigner(apiKey, secretKey, passphrase): фабрика Signer'а; пустой ключ
    создаёт `signer.enabled = false`, в этом случае подпись отдельных запросов
    блокируется (см. SignerError).
  - (Signer).Sign(timestamp, method, requestPath, body) (signature, error):
    собственно подпись.
  - (Signer).IsoTimestamp(now): возвращает timestamp в формате OKX.
  - (Signer).Credentials(): возвращает (apiKey, passphrase, enabled). Secret
    наружу НЕ отдаётся.

ВАЖНО ПО БЕЗОПАСНОСТИ:
  - SecretKey хранится внутри Signer и не сериализуется. В строковом
    представлении (для логов/паник) Signer возвращает редактированный вывод.
  - Логировать значения преподписи и тело запроса не следует — это утечка.

ЗАВИСИМОСТИ:
- crypto/hmac, crypto/sha256: подпись.
- encoding/base64:            кодирование.
- errors, fmt:                ошибки.
- time:                       формат timestamp.
*/

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// ErrSignerDisabled возвращается, когда вызывается Sign на пустых credentials.
var ErrSignerDisabled = errors.New("auth: signer is disabled (api key/secret/passphrase not configured)")

// Signer — компактный subj подписи запросов OKX. Безопасен для параллельного
// использования: внутри только read-only поля.
type Signer struct {
	apiKey     string
	secretKey  []byte
	passphrase string
	enabled    bool
}

// NewSigner создаёт Signer. Если хотя бы одно из полей пустое — signer
// помечается disabled (Sign будет возвращать ErrSignerDisabled). Это позволяет
// тому же Client'у обслуживать публичные эндпоинты OKX без credentials.
func NewSigner(apiKey, secretKey, passphrase string) *Signer {
	var enabled bool = apiKey != "" && secretKey != "" && passphrase != ""
	return &Signer{
		apiKey:     apiKey,
		secretKey:  []byte(secretKey),
		passphrase: passphrase,
		enabled:    enabled,
	}
}

// Enabled возвращает true, если signer готов подписывать запросы.
func (s *Signer) Enabled() bool { return s != nil && s.enabled }

// APIKey возвращает api key (для заголовка OK-ACCESS-KEY).
func (s *Signer) APIKey() string {
	if s == nil {
		return ""
	}
	return s.apiKey
}

// Passphrase возвращает passphrase (для заголовка OK-ACCESS-PASSPHRASE).
func (s *Signer) Passphrase() string {
	if s == nil {
		return ""
	}
	return s.passphrase
}

/*
Sign возвращает base64(HMAC_SHA256(secret, prehash)) согласно спецификации
OKX v5. Формат preHash:

	timestamp + method + requestPath + body

Параметры:
  - timestamp:   ISO8601 в UTC, см. IsoTimestamp.
  - method:      HTTP-метод в UPPER-CASE, "GET" / "POST" / "PUT" / "DELETE".
  - requestPath: путь относительно домена, начинается с '/', включает '?...'
                 для GET-запросов (точно та же query-строка, что попадёт в URL).
  - body:        тело JSON для POST/PUT; пустая строка для GET/DELETE без тела.

Возвращает base64-строку подписи или ErrSignerDisabled, если signer выключен.
*/
func (s *Signer) Sign(timestamp, method, requestPath, body string) (string, error) {
	if !s.Enabled() {
		return "", ErrSignerDisabled
	}

	var sb strings.Builder
	sb.Grow(len(timestamp) + len(method) + len(requestPath) + len(body))
	sb.WriteString(timestamp)
	sb.WriteString(method)
	sb.WriteString(requestPath)
	sb.WriteString(body)

	var mac = hmac.New(sha256.New, s.secretKey)
	mac.Write([]byte(sb.String()))
	var digest []byte = mac.Sum(nil)

	return base64.StdEncoding.EncodeToString(digest), nil
}

// IsoTimestamp форматирует now в формат OKX: "2006-01-02T15:04:05.000Z" в UTC.
// Если now zero — берётся time.Now().
func (s *Signer) IsoTimestamp(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC().Format("2006-01-02T15:04:05.000Z")
}

// String возвращает безопасное для логов представление Signer'а — без секретов.
func (s *Signer) String() string {
	if s == nil || !s.enabled {
		return "auth.Signer{disabled}"
	}
	return "auth.Signer{enabled, apiKey=" + redact(s.apiKey) + "}"
}

// redact превращает строку в "abcd…wxyz" — первые/последние 4 символа.
// Используется только для логов.
func redact(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "…" + s[len(s)-4:]
}
