/*
FILE: types/estimated-price.go

DESCRIPTION:
EstimatedPrice — expected delivery/exercise price for FUTURES (delivery
price at expiration) or OPTION (settlement price). Available only in the
~1 hour window before expiration.

Mapped from:
  - GET /api/v5/public/estimated-price?instId=...

Not applicable to SPOT/SWAP (perpetuals have no expiration).
*/

package types

import "github.com/shopspring/decimal"

// EstimatedPrice — expected delivery/exercise price.
type EstimatedPrice struct {
	// InstType — instrument type (FUTURES/OPTION).
	InstType InstType
	// InstID — instrument identifier.
	InstID string
	// SettlePx — expected settlement price (settlePx).
	SettlePx decimal.Decimal
	// Ts — settlement timestamp in ms (ts).
	Ts int64
}
