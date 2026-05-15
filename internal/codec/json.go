/*
ФАЙЛ: internal/codec/json.go

ОПИСАНИЕ:
Файл codec/json.go — единая точка подключения JSON-парсера в SDK. Все hot-path
вызовы внутри SDK ходят сюда, чтобы можно было одной заменой переключиться
с json-iterator на любой другой парсер (например, bytedance/sonic), если в
бенчмарках это даст преимущество.

ОСНОВНЫЕ ФУНКЦИИ:
  - Marshal/Unmarshal     — обёртки над выбранным парсером (json-iterator, ConfigCompatibleWithStandardLibrary).
  - NewDecoder            — обёртка для streaming-разбора (тело REST-ответа).
  - ParseDecimal          — string → decimal.Decimal без аллокации лишних обёрток.
                            Пустая строка трактуется как decimal.Zero (OKX часто
                            присылает пустую строку вместо нуля для опциональных полей).
  - ParseInt64            — string → int64. Пустая строка → 0.
  - ParseFloat64          — string → float64. Пустая строка → 0.

ЗАВИСИМОСТИ:
- github.com/json-iterator/go: ConfigCompatibleWithStandardLibrary даёт API,
  идентичный encoding/json, что упрощает миграцию и тестирование.
- github.com/shopspring/decimal: целевой тип для цен и количеств в hot-path.
*/

package codec

import (
	"io"
	"strconv"

	jsoniter "github.com/json-iterator/go"
	"github.com/shopspring/decimal"
)

// json — переиспользуемый экземпляр парсера. ConfigCompatibleWithStandardLibrary
// важен потому, что:
//   - корректно обрабатывает float64 (json-iterator default обрезает точность);
//   - совместим по поведению с тестовыми ожиданиями стандартной библиотеки;
//   - всё ещё ~4x быстрее encoding/json по нашим целям.
var json = jsoniter.ConfigCompatibleWithStandardLibrary

// RawMessage — аналог json.RawMessage, корректно работающий с jsoniter.
// Используется как поле в envelope-структурах, чтобы не парсить data дважды:
// сначала envelope с RawMessage, потом payload в свой типизированный dest.
type RawMessage []byte

// MarshalJSON реализует json.Marshaler.
func (m RawMessage) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("null"), nil
	}
	return []byte(m), nil
}

// UnmarshalJSON реализует json.Unmarshaler. Делает копию payload'а внутрь
// принимающего слайса, чтобы избежать «висящих» ссылок на read-buffer
// gorilla/websocket.
func (m *RawMessage) UnmarshalJSON(data []byte) error {
	*m = append((*m)[:0], data...)
	return nil
}

// Marshal сериализует значение в JSON.
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal парсит JSON в значение.
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// NewDecoder создаёт декодер для streaming-разбора reader'а.
func NewDecoder(r io.Reader) *jsoniter.Decoder {
	return json.NewDecoder(r)
}

// ParseDecimal превращает строку в decimal.Decimal. Пустая строка → Zero.
// Это удобно, потому что OKX часто шлёт пустые строки в опциональных полях
// (`""` вместо `"0"`) и каждый раз городить if не хочется.
func ParseDecimal(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(s)
}

// MustParseDecimal — как ParseDecimal, но при ошибке возвращает Zero и пишет
// флаг в out (если не nil). Используется в горячих парсерах, где мы НЕ хотим
// прерывать общее декодирование из-за одного битого поля.
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

// ParseInt64 превращает строку в int64. Пустая строка → 0.
func ParseInt64(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// ParseFloat64 превращает строку в float64. Пустая строка → 0.
func ParseFloat64(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
