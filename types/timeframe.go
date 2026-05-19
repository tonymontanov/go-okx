/*
FILE: types/timeframe.go

DESCRIPTION:
Timeframe for historical candles. Matches the value set of core/types in the
trading core and is accepted by both profiles (spot and swap).

SDK → OKX string mapping:

	1s/15s/30s            → not supported by REST; the calling code returns an
	                        error (sub-minute is aggregated upstream from aggTrade).
	1m, 3m, 5m, 15m, 30m → "1m"/"3m"/"5m"/"15m"/"30m"
	1h, 2h, 4h           → "1H"/"2H"/"4H"
	6h, 8h, 12h          → "6H"/"8H"/"12H"  (OKX defaults to Hong Kong time;
	                        use "6Hutc" for UTC — that is the responsibility of
	                        the specific MarketData client, not the enum).
	1d → "1D",  1w → "1W",  1mo → "1M"

The mapping itself lives in swap/market.go and spot/market.go (timeframeToOKX),
keeping the enum "clean" and reusable.
*/

package types

// Timeframe — candle timeframe.
type Timeframe string

const (
	// Sub-minute — supported only by aggregation at the upper layer; REST returns an error.
	Timeframe1s  Timeframe = "1s"
	Timeframe15s Timeframe = "15s"
	Timeframe30s Timeframe = "30s"

	Timeframe1m  Timeframe = "1m"
	Timeframe3m  Timeframe = "3m"
	Timeframe5m  Timeframe = "5m"
	Timeframe15m Timeframe = "15m"
	Timeframe30m Timeframe = "30m"

	Timeframe1h  Timeframe = "1h"
	Timeframe2h  Timeframe = "2h"
	Timeframe4h  Timeframe = "4h"
	Timeframe6h  Timeframe = "6h"
	Timeframe8h  Timeframe = "8h"
	Timeframe12h Timeframe = "12h"

	Timeframe1D  Timeframe = "1d"
	Timeframe1W  Timeframe = "1w"
	Timeframe1Mo Timeframe = "1mo"
)
