/*
FILE: internal/okxerr/errors.go

DESCRIPTION:
SDK error type + categories + OKX code mapping. Placed in an internal package
so that any internal/* package (rest, ws, codec) can use it without an import
cycle on the root okx package. The root okx package re-exports these entities
via type aliases.

See also: errors.go in the root (re-export).
*/

package okxerr

import (
	"errors"
	"fmt"
)

// ErrorKind — SDK error category.
type ErrorKind uint8

const (
	ErrorKindUnknown ErrorKind = iota
	ErrorKindNetwork
	ErrorKindRateLimit
	ErrorKindAuth
	ErrorKindInvalidRequest
	ErrorKindExchange
)

// String — human-readable category name.
func (k ErrorKind) String() string {
	switch k {
	case ErrorKindNetwork:
		return "network"
	case ErrorKindRateLimit:
		return "rate_limit"
	case ErrorKindAuth:
		return "auth"
	case ErrorKindInvalidRequest:
		return "invalid_request"
	case ErrorKindExchange:
		return "exchange"
	default:
		return "unknown"
	}
}

// Error — unified SDK error type.
type Error struct {
	Kind       ErrorKind
	HTTPStatus int
	OKXCode    string
	Message    string
	Cause      error
}

// Error implements the error interface.
func (e *Error) Error() string {
	switch {
	case e.OKXCode != "" && e.Cause != nil:
		return fmt.Sprintf("okx %s: code=%s status=%d msg=%q: %v", e.Kind, e.OKXCode, e.HTTPStatus, e.Message, e.Cause)
	case e.OKXCode != "":
		return fmt.Sprintf("okx %s: code=%s status=%d msg=%q", e.Kind, e.OKXCode, e.HTTPStatus, e.Message)
	case e.Cause != nil:
		return fmt.Sprintf("okx %s: status=%d msg=%q: %v", e.Kind, e.HTTPStatus, e.Message, e.Cause)
	default:
		return fmt.Sprintf("okx %s: status=%d msg=%q", e.Kind, e.HTTPStatus, e.Message)
	}
}

// Unwrap — for errors.Is/As.
func (e *Error) Unwrap() error { return e.Cause }

// New creates a *Error.
func New(kind ErrorKind, code, msg string, cause error) *Error {
	return &Error{Kind: kind, OKXCode: code, Message: msg, Cause: cause}
}

// IsNetwork returns true if err has category Network.
func IsNetwork(err error) bool { return matchKind(err, ErrorKindNetwork) }

// IsRateLimit returns true if err has category RateLimit.
func IsRateLimit(err error) bool { return matchKind(err, ErrorKindRateLimit) }

// IsAuth returns true if err has category Auth.
func IsAuth(err error) bool { return matchKind(err, ErrorKindAuth) }

// IsInvalidRequest returns true if err has category InvalidRequest.
func IsInvalidRequest(err error) bool { return matchKind(err, ErrorKindInvalidRequest) }

// IsExchange returns true if err has category Exchange.
func IsExchange(err error) bool { return matchKind(err, ErrorKindExchange) }

func matchKind(err error, kind ErrorKind) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind == kind
	}
	return false
}

/*
MapOKXCode returns the SDK error category for a specific OKX code.

Covered groups:
  - 50011, 50061     — rate limit;
  - 50100..50114     — authentication / permission / IP whitelist;
  - 51000..51020, 51119 — invalid params;
  - 51008            — insufficient funds (Exchange);
  - everything else  — ErrorKindExchange.
*/
func MapOKXCode(code, msg string) ErrorKind {
	_ = msg
	switch code {
	case "0":
		return ErrorKindUnknown
	case "50011", "50061":
		return ErrorKindRateLimit
	case
		"50100", "50101", "50102", "50103", "50104", "50105", "50106",
		"50107", "50108", "50109", "50110", "50111", "50112", "50113", "50114":
		return ErrorKindAuth
	case "51008":
		return ErrorKindExchange
	case
		"51000", "51001", "51002", "51003", "51004", "51005", "51006",
		"51007", "51009", "51010", "51011", "51012", "51014", "51015",
		"51016", "51020", "51119":
		return ErrorKindInvalidRequest
	default:
		return ErrorKindExchange
	}
}

// MapHTTPStatus returns the SDK error category for an HTTP status code (when
// the response body is invalid or does not contain an OKX code).
func MapHTTPStatus(status int) ErrorKind {
	switch {
	case status == 429:
		return ErrorKindRateLimit
	case status == 401 || status == 403:
		return ErrorKindAuth
	case status >= 500:
		return ErrorKindNetwork
	case status >= 400:
		return ErrorKindInvalidRequest
	default:
		return ErrorKindUnknown
	}
}
