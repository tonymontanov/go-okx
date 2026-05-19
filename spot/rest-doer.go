/*
FILE: spot/rest-doer.go

DESCRIPTION:
Minimal REST transport interface for SPOT sub-clients. Mirrors swap/rest-doer.go —
the same contract as internal/rest.Client.Do. Used for testability (injecting
a fake REST transport in unit tests).
*/

package spot

import (
	"context"

	"github.com/tonymontanov/go-okx/v2/internal/rest"
)

// restDoer — minimal REST transport contract.
type restDoer interface {
	Do(ctx context.Context, opts rest.Options) (rest.Response, map[string]string, error)
}
