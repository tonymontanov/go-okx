/*
ФАЙЛ: errors.go

ОПИСАНИЕ:
Публичный реэкспорт сущностей пакета internal/okxerr. Сам тип, категории и
маппинги живут в internal/okxerr (см. документацию там); здесь — только
type alias и переменные-функции, чтобы пользователь работал через привычный
импорт корневого пакета:

    import okx "github.com/tonymontanov/go-okx/v2"

    if okx.IsRateLimit(err) { ... }
*/

package okx

import "github.com/tonymontanov/go-okx/v2/internal/okxerr"

// Error — тип ошибки SDK. Alias.
type Error = okxerr.Error

// ErrorKind — категория ошибки SDK. Alias.
type ErrorKind = okxerr.ErrorKind

// Категории. Объявлены как типизированные константы через alias.
const (
	ErrorKindUnknown        = okxerr.ErrorKindUnknown
	ErrorKindNetwork        = okxerr.ErrorKindNetwork
	ErrorKindRateLimit      = okxerr.ErrorKindRateLimit
	ErrorKindAuth           = okxerr.ErrorKindAuth
	ErrorKindInvalidRequest = okxerr.ErrorKindInvalidRequest
	ErrorKindExchange       = okxerr.ErrorKindExchange
)

// NewError создаёт *Error.
func NewError(kind ErrorKind, code, msg string, cause error) *Error {
	return okxerr.New(kind, code, msg, cause)
}

// IsNetwork / IsRateLimit / IsAuth / IsInvalidRequest / IsExchange — предикаты
// категории ошибки.
func IsNetwork(err error) bool        { return okxerr.IsNetwork(err) }
func IsRateLimit(err error) bool      { return okxerr.IsRateLimit(err) }
func IsAuth(err error) bool           { return okxerr.IsAuth(err) }
func IsInvalidRequest(err error) bool { return okxerr.IsInvalidRequest(err) }
func IsExchange(err error) bool       { return okxerr.IsExchange(err) }

// MapOKXCode возвращает категорию ошибки SDK для биржевого кода.
func MapOKXCode(code, msg string) ErrorKind { return okxerr.MapOKXCode(code, msg) }

// MapHTTPStatus возвращает категорию ошибки SDK для HTTP-статуса.
func MapHTTPStatus(status int) ErrorKind { return okxerr.MapHTTPStatus(status) }
