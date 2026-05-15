/*
ФАЙЛ: logger.go

ОПИСАНИЕ:
Публичный реэкспорт интерфейса Logger и типизированных Field-фабрик. Сам
интерфейс/тип живут в internal/okxlog (см. документацию там); здесь —
type alias и тонкие функции-обёртки.
*/

package okx

import "github.com/tonymontanov/go-okx/v2/internal/okxlog"

// Logger — интерфейс логирования SDK. Alias.
type Logger = okxlog.Logger

// Field — типизированное поле лога. Alias.
type Field = okxlog.Field

// FieldKind — дискриминатор Field. Alias.
type FieldKind = okxlog.FieldKind

// Значения FieldKind.
const (
	FieldKindString = okxlog.FieldKindString
	FieldKindInt    = okxlog.FieldKindInt
	FieldKindFloat  = okxlog.FieldKindFloat
	FieldKindBool   = okxlog.FieldKindBool
	FieldKindError  = okxlog.FieldKindError
)

// NoopLogger возвращает no-op логгер. Используется как default.
func NoopLogger() Logger { return okxlog.Noop() }

// Str / Int / Float / Bool / Err — фабрики Field.
func Str(key, value string) Field   { return okxlog.Str(key, value) }
func Int(key string, v int64) Field { return okxlog.Int(key, v) }
func Float(key string, v float64) Field {
	return okxlog.Float(key, v)
}
func Bool(key string, v bool) Field { return okxlog.Bool(key, v) }
func Err(err error) Field           { return okxlog.Err(err) }
