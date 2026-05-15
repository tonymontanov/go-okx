/*
ФАЙЛ: internal/codec/json_test.go

ОПИСАНИЕ:
Тесты codec — парсеры строк в decimal/int64/float64 и Marshal/Unmarshal обёртки.
*/

package codec

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestParseDecimal_Empty(t *testing.T) {
	var d decimal.Decimal
	var err error
	d, err = ParseDecimal("")
	if err != nil {
		t.Fatalf("empty string must yield Zero without error, got err=%v", err)
	}
	if !d.IsZero() {
		t.Fatalf("expected Zero, got %s", d.String())
	}
}

func TestParseDecimal_Normal(t *testing.T) {
	var d decimal.Decimal
	var err error
	d, err = ParseDecimal("12345.6789")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Equal(decimal.RequireFromString("12345.6789")) {
		t.Fatalf("unexpected value: %s", d.String())
	}
}

func TestParseDecimal_Invalid(t *testing.T) {
	var _, err = ParseDecimal("not-a-number")
	if err == nil {
		t.Fatalf("expected error for invalid input")
	}
}

func TestMustParseDecimal_Invalid_ReturnsZero(t *testing.T) {
	var ok bool
	var d decimal.Decimal = MustParseDecimal("nope", &ok)
	if ok {
		t.Fatalf("expected ok=false for invalid input")
	}
	if !d.IsZero() {
		t.Fatalf("expected Zero, got %s", d.String())
	}
}

func TestParseInt64_EmptyAndValid(t *testing.T) {
	var v int64
	var err error
	v, err = ParseInt64("")
	if err != nil || v != 0 {
		t.Fatalf("empty must be 0, no err; got v=%d err=%v", v, err)
	}
	v, err = ParseInt64("1735776123456")
	if err != nil || v != 1735776123456 {
		t.Fatalf("unexpected: v=%d err=%v", v, err)
	}
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	type s struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	var input s = s{A: 1, B: "hello"}
	var raw []byte
	var err error
	raw, err = Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var out s
	if err = Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out != input {
		t.Fatalf("round-trip mismatch: in=%v out=%v", input, out)
	}
}
