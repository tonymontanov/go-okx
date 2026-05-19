/*
FILE: errors.go

DESCRIPTION:
Public re-export of entities from the internal/okxerr package. The type itself,
categories, and mappings live in internal/okxerr (see documentation there);
here — only the type alias and function variables so that the user works through
the familiar root-package import:

    import okx "github.com/tonymontanov/go-okx/v2"

    if okx.IsRateLimit(err) { ... }
*/

package okx

import "github.com/tonymontanov/go-okx/v2/internal/okxerr"

// Error — SDK error type. Alias.
type Error = okxerr.Error

// ErrorKind — SDK error category. Alias.
type ErrorKind = okxerr.ErrorKind

// Categories. Declared as typed constants via alias.
const (
	ErrorKindUnknown        = okxerr.ErrorKindUnknown
	ErrorKindNetwork        = okxerr.ErrorKindNetwork
	ErrorKindRateLimit      = okxerr.ErrorKindRateLimit
	ErrorKindAuth           = okxerr.ErrorKindAuth
	ErrorKindInvalidRequest = okxerr.ErrorKindInvalidRequest
	ErrorKindExchange       = okxerr.ErrorKindExchange
)

// NewError creates a *Error.
func NewError(kind ErrorKind, code, msg string, cause error) *Error {
	return okxerr.New(kind, code, msg, cause)
}

// IsNetwork / IsRateLimit / IsAuth / IsInvalidRequest / IsExchange — error
// category predicates.
func IsNetwork(err error) bool        { return okxerr.IsNetwork(err) }
func IsRateLimit(err error) bool      { return okxerr.IsRateLimit(err) }
func IsAuth(err error) bool           { return okxerr.IsAuth(err) }
func IsInvalidRequest(err error) bool { return okxerr.IsInvalidRequest(err) }
func IsExchange(err error) bool       { return okxerr.IsExchange(err) }

// MapOKXCode returns the SDK error category for an exchange code.
func MapOKXCode(code, msg string) ErrorKind { return okxerr.MapOKXCode(code, msg) }

// MapHTTPStatus returns the SDK error category for an HTTP status code.
func MapHTTPStatus(status int) ErrorKind { return okxerr.MapHTTPStatus(status) }
