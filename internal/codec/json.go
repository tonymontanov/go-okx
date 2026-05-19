/*
FILE: internal/codec/json.go

DESCRIPTION:
codec/json.go is the single JSON parser entry point in the SDK. All hot-path
calls inside the SDK go through here so that switching from json-iterator to
any other parser (e.g. bytedance/sonic) requires only one change if benchmarks
show an advantage.

MAIN FUNCTIONS:
  - Marshal/Unmarshal     — wrappers over the selected parser (json-iterator, ConfigCompatibleWithStandardLibrary).
  - NewDecoder            — wrapper for streaming parsing (REST response body).
  - ParseDecimal          — string → decimal.Decimal without extra allocations.
                            An empty string is treated as decimal.Zero (OKX frequently
                            sends an empty string instead of zero for optional fields).
  - ParseInt64            — string → int64. Empty string → 0.
  - ParseFloat64          — string → float64. Empty string → 0.

DEPENDENCIES:
- github.com/json-iterator/go: ConfigCompatibleWithStandardLibrary provides an API
  identical to encoding/json, simplifying migration and testing.
- github.com/shopspring/decimal: target type for prices and quantities in hot paths.
*/

package codec

import (
	"io"
	"strconv"

	jsoniter "github.com/json-iterator/go"
	"github.com/shopspring/decimal"
)

// json — reusable parser instance. ConfigCompatibleWithStandardLibrary matters because:
//   - correctly handles float64 (json-iterator default truncates precision);
//   - behaviorally compatible with standard library test expectations;
//   - still ~4x faster than encoding/json for our use case.
var json = jsoniter.ConfigCompatibleWithStandardLibrary

// RawMessage — analogue of json.RawMessage that works correctly with jsoniter.
// Used as a field in envelope structs to avoid parsing data twice:
// first the envelope with RawMessage, then the payload into its typed destination.
type RawMessage []byte

// MarshalJSON implements json.Marshaler.
func (m RawMessage) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("null"), nil
	}
	return []byte(m), nil
}

// UnmarshalJSON implements json.Unmarshaler. Copies the payload into the
// receiving slice to avoid dangling references to the gorilla/websocket
// read buffer.
func (m *RawMessage) UnmarshalJSON(data []byte) error {
	*m = append((*m)[:0], data...)
	return nil
}

// Marshal serializes a value to JSON.
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal parses JSON into a value.
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// NewDecoder creates a decoder for streaming parsing of a reader.
func NewDecoder(r io.Reader) *jsoniter.Decoder {
	return json.NewDecoder(r)
}

// ParseDecimal converts a string to decimal.Decimal. Empty string → Zero.
// Convenient because OKX frequently sends empty strings in optional fields
// (`""` instead of `"0"`), avoiding repeated nil checks.
func ParseDecimal(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(s)
}

// MustParseDecimal — like ParseDecimal, but on error returns Zero and sets
// the flag in out (if not nil). Used in hot parsers where we do NOT want
// to abort the entire decode due to a single malformed field.
func MustParseDecimal(s string, ok *bool) decimal.Decimal {
	if s == "" {
		if ok != nil {
			*ok = true
		}
		return decimal.Zero
	}
	var d decimal.Decimal
	var err error
	d, err = decimal.NewFromString(s)
	if ok != nil {
		*ok = err == nil
	}
	if err != nil {
		return decimal.Zero
	}
	return d
}

// ParseInt64 converts a string to int64. Empty string → 0.
func ParseInt64(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// ParseFloat64 converts a string to float64. Empty string → 0.
func ParseFloat64(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
