/*
FILE: types/max-order-size.go

DESCRIPTION:
MaxOrderSize — maximum allowable order size for an instrument in the
specified margin mode. Takes into account the current balance, leverage,
and existing open positions/orders.

Mapped from:
  - GET /api/v5/account/max-size       — max size WITHOUT pending orders
  - GET /api/v5/account/max-avail-size — max size WITH pending orders

HFT usage:
  - pre-trade sizing: query before a series of orders to avoid
    51008 (insufficient balance) or 51020 (max position size exceeded);
  - rebalance: compute the difference between "max long" and "max short"
    to understand current margin utilization.
*/

package types

import "github.com/shopspring/decimal"

// MaxOrderSize — maximum order size.
type MaxOrderSize struct {
	// InstID — instrument.
	InstID string
	// Ccy — currency (for MARGIN/SPOT) (ccy).
	Ccy string
	// MaxBuy — maximum buy order size in base currency/contracts (maxBuy).
	MaxBuy decimal.Decimal
	// MaxSell — maximum sell order size (maxSell).
	MaxSell decimal.Decimal
}

// MaxOrderSizes — slice.
type MaxOrderSizes []MaxOrderSize
