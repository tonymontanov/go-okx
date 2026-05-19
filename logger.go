/*
FILE: logger.go

DESCRIPTION:
Public re-export of the Logger interface and typed Field factories. The
interface/type itself lives in internal/okxlog (see documentation there);
here — type aliases and thin wrapper functions.
*/

package okx

import "github.com/tonymontanov/go-okx/v2/internal/okxlog"

// Logger — SDK logging interface. Alias.
type Logger = okxlog.Logger

// Field — typed log field. Alias.
type Field = okxlog.Field

// FieldKind — Field discriminator. Alias.
type FieldKind = okxlog.FieldKind

// FieldKind values.
const (
	FieldKindString = okxlog.FieldKindString
	FieldKindInt    = okxlog.FieldKindInt
	FieldKindFloat  = okxlog.FieldKindFloat
	FieldKindBool   = okxlog.FieldKindBool
	FieldKindError  = okxlog.FieldKindError
)

// NoopLogger returns a no-op logger. Used as the default.
func NoopLogger() Logger { return okxlog.Noop() }

// Str / Int / Float / Bool / Err — Field factories.
func Str(key, value string) Field   { return okxlog.Str(key, value) }
func Int(key string, v int64) Field { return okxlog.Int(key, v) }
func Float(key string, v float64) Field {
	return okxlog.Float(key, v)
}
func Bool(key string, v bool) Field { return okxlog.Bool(key, v) }
func Err(err error) Field           { return okxlog.Err(err) }
