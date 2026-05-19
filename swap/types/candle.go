/*
FILE: swap/types/candle.go

DESCRIPTION:
Historical candle structs for the SWAP profile. Since the common layer was
extracted — a type alias for commontypes.Candle/Candles. Documentation is
in types/candle.go.

Volume unit for SWAP — contracts (multiply by ctVal to convert to base).
*/

package types

import commontypes "github.com/tonymontanov/go-okx/v2/types"

// Candle — one candle. See commontypes.Candle.
type Candle = commontypes.Candle

// Candles — slice of candles. See commontypes.Candles.
type Candles = commontypes.Candles
