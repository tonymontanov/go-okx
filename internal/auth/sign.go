/*
FILE: internal/auth/sign.go

DESCRIPTION:
sign.go implements request signing for OKX v5 REST/WS. Algorithm per the
official documentation:

  preHash = timestamp + method + requestPath + body
  signature = base64( HMAC_SHA256(secretKey, preHash) )

Where:
  - timestamp     — ISO8601 in UTC with milliseconds and 'Z' suffix,
                    e.g. "2026-05-15T16:18:00.123Z".
  - method        — UPPER-CASE HTTP method ("GET" / "POST").
  - requestPath   — URL.Path plus canonicalized query string (with '?'),
                    e.g. "/api/v5/trade/order" or
                    "/api/v5/market/books?instId=BTC-USDT-SWAP&sz=20".
  - body          — JSON body for POST / empty string for GET.

The same 4 headers OK-ACCESS-KEY / SIGN / TIMESTAMP / PASSPHRASE are sent
with signed REST requests and equivalently in the WS login message payload.

MAIN FUNCTIONS:
  - NewSigner(apiKey, secretKey, passphrase): Signer factory; an empty key
    creates `signer.enabled = false`, in which case signing individual requests
    is blocked (see SignerError).
  - (Signer).Sign(timestamp, method, requestPath, body) (signature, error):
    the actual signing.
  - (Signer).IsoTimestamp(now): returns the timestamp in OKX format.
  - (Signer).Credentials(): returns (apiKey, passphrase, enabled). Secret is
    NOT exposed.

SECURITY NOTES:
  - SecretKey is stored inside Signer and is not serialized. The string
    representation (for logs/panics) returns a redacted output.
  - Do not log pre-hash values or request bodies — this is a secret leak.

DEPENDENCIES:
- crypto/hmac, crypto/sha256: signing.
- encoding/base64:            encoding.
- errors, fmt:                errors.
- time:                       timestamp formatting.
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

// ErrSignerDisabled is returned when Sign is called with empty credentials.
var ErrSignerDisabled = errors.New("auth: signer is disabled (api key/secret/passphrase not configured)")

// Signer — compact OKX request signer. Safe for concurrent use: contains
// only read-only fields.
type Signer struct {
	apiKey     string
	secretKey  []byte
	passphrase string
	enabled    bool
}

// NewSigner creates a Signer. If any field is empty, the signer is marked
// disabled (Sign will return ErrSignerDisabled). This allows the same Client
// to serve public OKX endpoints without credentials.
func NewSigner(apiKey, secretKey, passphrase string) *Signer {
	var enabled bool = apiKey != "" && secretKey != "" && passphrase != ""
	return &Signer{
		apiKey:     apiKey,
		secretKey:  []byte(secretKey),
		passphrase: passphrase,
		enabled:    enabled,
	}
}

// Enabled returns true if the signer is ready to sign requests.
func (s *Signer) Enabled() bool { return s != nil && s.enabled }

// APIKey returns the API key (for the OK-ACCESS-KEY header).
func (s *Signer) APIKey() string {
	if s == nil {
		return ""
	}
	return s.apiKey
}

// Passphrase returns the passphrase (for the OK-ACCESS-PASSPHRASE header).
func (s *Signer) Passphrase() string {
	if s == nil {
		return ""
	}
	return s.passphrase
}

/*
Sign returns base64(HMAC_SHA256(secret, prehash)) per OKX v5 specification.
preHash format:

	timestamp + method + requestPath + body

Parameters:
  - timestamp:   ISO8601 in UTC, see IsoTimestamp.
  - method:      UPPER-CASE HTTP method, "GET" / "POST" / "PUT" / "DELETE".
  - requestPath: path relative to the domain, starts with '/', includes '?...'
                 for GET requests (exactly the same query string that goes in the URL).
  - body:        JSON body for POST/PUT; empty string for GET/DELETE without a body.

Returns the base64 signature string or ErrSignerDisabled if the signer is disabled.
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

// IsoTimestamp formats now in OKX format: "2006-01-02T15:04:05.000Z" in UTC.
// If now is zero, time.Now() is used.
func (s *Signer) IsoTimestamp(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC().Format("2006-01-02T15:04:05.000Z")
}

// String returns a log-safe representation of the Signer — without secrets.
func (s *Signer) String() string {
	if s == nil || !s.enabled {
		return "auth.Signer{disabled}"
	}
	return "auth.Signer{enabled, apiKey=" + redact(s.apiKey) + "}"
}

// redact turns a string into "abcd…wxyz" — first/last 4 characters.
// Used for logging only.
func redact(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "…" + s[len(s)-4:]
}
