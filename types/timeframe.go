/*
ФАЙЛ: types/timeframe.go

ОПИСАНИЕ:
Таймфрейм исторических свечей. Совпадает по набору значений с core/types
торгового ядра и принимается обоими профилями (spot и swap).

Маппинг SDK → строка OKX:

	1s/15s/30s            → REST не поддерживает; возвращается ошибка вызывающим
	                        кодом (sub-minute агрегируется наверху из aggTrade).
	1m, 3m, 5m, 15m, 30m → "1m"/"3m"/"5m"/"15m"/"30m"
	1h, 2h, 4h           → "1H"/"2H"/"4H"
	6h, 8h, 12h          → "6H"/"8H"/"12H"  (Hong Kong time у OKX по умолчанию;
	                        для UTC использовать "6Hutc" — это уже задача
	                        конкретного MarketData-клиента, не enum'а).
	1d → "1D",  1w → "1W",  1mo → "1M"

Сам маппинг живёт в swap/market.go и spot/market.go (timeframeToOKX),
чтобы enum оставался "чистым" и переиспользуемым.
*/

package types

// Timeframe — таймфрейм свечи.
type Timeframe string

const (
	// Sub-minute — поддерживается только агрегацией наверху, REST вернёт ошибку.
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
