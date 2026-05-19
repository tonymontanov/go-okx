/*
FILE: swap/rest-doer.go

DESCRIPTION:
Declares the minimal restDoer interface for making REST calls and parsing the
common OKX wrapper. Using an interface (rather than *rest.Client directly)
provides two benefits:
  1. Testability: in unit tests for sub-clients we pass a fake REST without
     a real http.Client.
  2. Isolation: swap package code does not depend on transport implementation
     details — the REST client could theoretically be swapped for an async pipeline.

restDoer exactly matches the public API of *internal/rest.Client.Do.
*/

package swap

import (
	"context"

	"github.com/tonymontanov/go-okx/v2/internal/rest"
)

// restDoer — minimal REST transport contract.
type restDoer interface {
	Do(ctx context.Context, opts rest.Options) (rest.Response, map[string]string, error)
}
