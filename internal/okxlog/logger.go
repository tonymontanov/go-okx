/*
FILE: internal/okxlog/logger.go

DESCRIPTION:
Logger interface + typed Field. Placed in internal so that any SDK package
(rest, ws, codec, swap) can use it without import cycles. The root okx package
re-exports the type/functions via aliases.
*/

package okxlog

// FieldKind — Field discriminator.
type FieldKind uint8

const (
	FieldKindString FieldKind = iota
	FieldKindInt
	FieldKindFloat
	FieldKindBool
	FieldKindError
)

// Field — typed key-value log field. No interface — no boxing.
type Field struct {
	Key  string
	Kind FieldKind
	Str  string
	Int  int64
	Flt  float64
	Bool bool
	Err  error
}

// Str / Int / Float / Bool / Err — Field factories.
func Str(key, value string) Field {
	return Field{Key: key, Kind: FieldKindString, Str: value}
}
func Int(key string, value int64) Field {
	return Field{Key: key, Kind: FieldKindInt, Int: value}
}
func Float(key string, value float64) Field {
	return Field{Key: key, Kind: FieldKindFloat, Flt: value}
}
func Bool(key string, value bool) Field {
	return Field{Key: key, Kind: FieldKindBool, Bool: value}
}
func Err(err error) Field {
	return Field{Key: "error", Kind: FieldKindError, Err: err}
}

// Logger — minimal SDK logging contract.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
}

// noop — default implementation.
type noop struct{}

// Noop returns the singleton no-op logger.
func Noop() Logger { return noopSingleton }

var noopSingleton Logger = noop{}

func (noop) Debug(string, ...Field) {}
func (noop) Info(string, ...Field)  {}
func (noop) Warn(string, ...Field)  {}
func (noop) Error(string, ...Field) {}
