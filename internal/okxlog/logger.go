/*
ФАЙЛ: internal/okxlog/logger.go

ОПИСАНИЕ:
Интерфейс Logger + типизированное Field. Вынесен в internal, чтобы любой
пакет SDK (rest, ws, codec, swap) использовал его без import-cycle. Корневой
пакет okx переэкспортирует тип/функции через alias.
*/

package okxlog

// FieldKind — дискриминатор Field.
type FieldKind uint8

const (
	FieldKindString FieldKind = iota
	FieldKindInt
	FieldKindFloat
	FieldKindBool
	FieldKindError
)

// Field — типизированное key-value поле лога. Без интерфейса — без boxing.
type Field struct {
	Key  string
	Kind FieldKind
	Str  string
	Int  int64
	Flt  float64
	Bool bool
	Err  error
}

// Str / Int / Float / Bool / Err — фабрики Field'ов.
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

// Logger — минимальный контракт логирования SDK.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
}

// noop — реализация по умолчанию.
type noop struct{}

// Noop возвращает singleton no-op логгер.
func Noop() Logger { return noopSingleton }

var noopSingleton Logger = noop{}

func (noop) Debug(string, ...Field) {}
func (noop) Info(string, ...Field)  {}
func (noop) Warn(string, ...Field)  {}
func (noop) Error(string, ...Field) {}
