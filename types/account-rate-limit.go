/*
FILE: types/account-rate-limit.go

DESCRIPTION:
Common (profile-agnostic) data type for the response of OKX endpoint
GET /api/v5/account/rate-limit — "Get account rate limit" in the official
v5 documentation. The endpoint is global (not bound to instType=SPOT/SWAP),
so the type lives in the neutral types/ package and is re-exported via
aliases into spot/types and swap/types.

WHY IT MATTERS (HFT-context):
OKX REST does NOT return rate-limit response headers (no equivalent of
Binance's X-MBX-USED-WEIGHT-1M / X-MBX-ORDER-COUNT-10S). This endpoint is
OKX's official mechanism for retrieving live, account-specific rate-limit
information: it returns the current sub-account limit, the upcoming
adjusted limit (for VIP5+), and the fill-ratio metrics that drive the
fill-based dynamic limit policy.

OKX REFRESHES THESE NUMBERS DAILY AT 08:00 UTC:
The exchange recalculates fill ratios off the previous 7-day window and
applies the corresponding rate-limit tier to every sub-account once per
day, at 08:00 UTC. A consumer that wants a proactive rate-limiter should
poll this endpoint at least once per day (typically shortly after 08:00 UTC)
and refresh its sub-account budget accordingly. Polling more frequently is
allowed but redundant — server-side numbers do not change between updates.

FIELDS COME AS STRINGS ON THE WIRE:
OKX serialises everything as strings. AccRateLimit and NextAccRateLimit
are integer order counts (per 2 seconds); FillRatio and MainFillRatio are
dimensionless decimals. Empty strings (typical for non-VIP5 accounts that
do not receive NextAccRateLimit / FillRatio) are parsed as zero values
by the unmarshal layer in spot/account.go and swap/account.go.
*/

package types

import "github.com/shopspring/decimal"

// AccountRateLimitInfo — typed response of GET /api/v5/account/rate-limit.
//
// LIFECYCLE NOTE:
// On startup, call GetAccountRateLimit once to align the local rate-limiter
// budget with the live sub-account limit. Then schedule a daily refresh
// shortly after 08:00 UTC (the OKX recalculation time). Between refreshes
// the numbers are stable, so additional polling is wasteful and counts
// against the per-endpoint rate limit ("query" category, ~20 req / 2s).
type AccountRateLimitInfo struct {
	// AccRateLimit — current sub-account rate limit, expressed as orders per
	// 2 seconds. This is the live value of the OKX "sub-account 1000 orders /
	// 2s" plane (error 50061 when exceeded). For non-VIP5 accounts the value
	// is the constant 1000; for VIP5+ it is dynamically adjusted by OKX based
	// on the previous-day fill ratio.
	AccRateLimit int

	// NextAccRateLimit — expected sub-account rate limit for the next daily
	// monitoring period (next 08:00 UTC). Populated only for VIP5+ accounts
	// — for everyone else OKX returns an empty string and this field is 0.
	// Useful when the rate-limiter wants to start throttling preemptively
	// before a known downgrade lands.
	NextAccRateLimit int

	// FillRatio — sub-account fill ratio computed off the past 7 days of
	// transactions. Dimensionless decimal in the range [0, 1]. Populated only
	// for VIP5+ accounts; zero Decimal otherwise.
	FillRatio decimal.Decimal

	// MainFillRatio — master-account aggregated fill ratio, computed off the
	// past 7 days across all sub-accounts. Populated only for VIP5+ accounts;
	// zero Decimal otherwise.
	MainFillRatio decimal.Decimal

	// Ts — server-side timestamp of the response, in milliseconds since the
	// Unix epoch. Useful for measuring clock skew between the SDK client and
	// the OKX servers and for ordering daily refresh ticks correctly.
	Ts int64
}
