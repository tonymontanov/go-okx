/*
FILE: swap/types/balance.go

DESCRIPTION:
Response model for GET /api/v5/account/balance for the SWAP profile. Since
the common layer was extracted — a type alias for commontypes.Balance/BalanceDetail
(OKX uses unified-account; the model is identical for spot and swap).

Documentation is in types/balance.go.
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// Balance — unified-account state. See commontypes.Balance.
type Balance = commontypes.Balance

// BalanceDetail — balance for a single currency. See commontypes.BalanceDetail.
type BalanceDetail = commontypes.BalanceDetail
