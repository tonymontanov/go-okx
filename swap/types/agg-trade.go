/*
FILE: swap/types/agg-trade.go

DESCRIPTION:
AggTrade for the SWAP profile. Since the common layer was extracted — a type alias
for commontypes.AggTrade. Documentation is in types/agg-trade.go.

Size unit for SWAP — contracts (multiply by ctVal to convert to base).
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// AggTrade — one trade from the trades stream. See commontypes.AggTrade.
type AggTrade = commontypes.AggTrade
