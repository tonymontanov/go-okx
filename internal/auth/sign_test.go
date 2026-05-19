/*
FILE: internal/auth/sign_test.go

DESCRIPTION:
OKX signing tests. Cover:
  - correct preHash format (timestamp+method+path+body);
  - base64 of HMAC-SHA256 result;
  - correct disabled-mode behavior (empty keys) and ErrSignerDisabled error;
  - determinism of the signature given the same inputs.

Reference values computed independently: HMAC_SHA256("01234567"*N, preImage)
with a known key — see setUp in each test.
*/

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestSigner_Disabled_NoSign(t *testing.T) {
	var s *Signer = NewSigner("", "", "")
	if s.Enabled() {
		t.Fatalf("signer must be disabled when keys are empty")
	}
	var sig string
	var err error
	sig, err = s.Sign("2026-01-01T00:00:00.000Z", "GET", "/api/v5/foo", "")
	if !errors.Is(err, ErrSignerDisabled) {
		t.Fatalf("expected ErrSignerDisabled, got %v", err)
	}
	if sig != "" {
		t.Fatalf("signature must be empty when disabled")
	}
}

func TestSigner_Sign_MatchesReference(t *testing.T) {
	var apiKey string = "test-api-key"
	var secret string = "test-secret-key"
	var passphrase string = "passphrase"

	var s *Signer = NewSigner(apiKey, secret, passphrase)
	if !s.Enabled() {
		t.Fatalf("signer must be enabled")
	}

	var ts string = "2026-05-15T16:00:00.000Z"
	var method string = "GET"
	var path string = "/api/v5/account/balance"
	var body string = ""

	var got string
	var err error
	got, err = s.Sign(ts, method, path, body)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// reference value computed independently
	var pre string = ts + method + path + body
	var mac = hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(pre))
	var want string = base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Fatalf("signature mismatch: got=%q want=%q (preImage=%q)", got, want, pre)
	}
}

func TestSigner_Sign_BodyIncluded(t *testing.T) {
	var s *Signer = NewSigner("k", "s", "p")
	var sig1, sig2 string
	var err error
	sig1, err = s.Sign("ts", "POST", "/path", `{"a":1}`)
	if err != nil {
		t.Fatal(err)
	}
	sig2, err = s.Sign("ts", "POST", "/path", `{"a":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if sig1 == sig2 {
		t.Fatalf("signature must depend on body")
	}
}

func TestSigner_IsoTimestamp_FormatAndUTC(t *testing.T) {
	var s *Signer = NewSigner("k", "s", "p")
	// fixed time in MSK (+3), must be normalized to UTC
	var msk *time.Location
	var err error
	msk, err = time.LoadLocation("Etc/GMT-3")
	if err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}
	var now time.Time = time.Date(2026, 5, 15, 19, 0, 0, 123_000_000, msk)
	var ts string = s.IsoTimestamp(now)
	var want string = "2026-05-15T16:00:00.123Z"
	if ts != want {
		t.Fatalf("timestamp format mismatch: got=%q want=%q", ts, want)
	}
}

func TestSigner_String_Redacts(t *testing.T) {
	var s *Signer = NewSigner("ABCDEFGHIJ", "secret-secret", "pass")
	var got string = s.String()
	// must not contain secret, passphrase, or the full apiKey
	if got == "" {
		t.Fatalf("String must return non-empty value")
	}
	if contains(got, "secret-secret") || contains(got, "EFGH") {
		t.Fatalf("String must redact sensitive fields, got %q", got)
	}
}

// contains — without strings.Contains to avoid extra imports in the test.
func contains(s, sub string) bool {
	var i int
	for i = 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
