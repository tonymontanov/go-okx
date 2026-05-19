/*
FILE: types/price-limit.go

DESCRIPTION:
PriceLimit — current upper and lower bounds of valid order prices for an
instrument. OKX rejects orders outside these bounds with code 51004.

Mapped from:
  - GET /api/v5/public/price-limit?instId=...
  - WS public channel "price-limit"

HFT usage:
  - pre-trade validation: verify that the order price falls within [SellLmt,
    BuyLmt] before sending to avoid 51004 and wasting rate-limit quota;
  - market phase detector: a sharp narrowing of the bounds often precedes a
    trading halt or a volatile move.
*/

package types

import "github.com/shopspring/decimal"

// PriceLimit — valid order price bounds for an instrument.
type PriceLimit struct {
	// InstType — instrument type.
	InstType InstType
	// InstID — instrument identifier.
	InstID string
	// BuyLmt — maximum price for a buy order (buyLmt).
	BuyLmt decimal.Decimal
	// SellLmt — minimum price for a sell order (sellLmt).
	SellLmt decimal.Decimal
	// Ts — snapshot timestamp in ms (ts).
	Ts int64
}
