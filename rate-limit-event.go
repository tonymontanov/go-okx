/*
FILE: rate-limit-event.go

DESCRIPTION:
Public RateLimitEvent type that the SDK delivers to subscribers via
okx.Config.RateLimitEventObserver. Added in v2.2.0 as a replacement/extension
of the old RateLimitObserver (see config.go).

WHY:
The OKX rate-limit model has three dimensions that CANNOT be derived from
(endpoint, headers) alone:

 1. The accounting unit on batch endpoints is an ORDER, not a REQUEST. One POST
    /api/v5/trade/batch-orders for 20 orders costs 20 units from the
    "300 orders per 2s" budget, not 1 unit from the non-existent "300 requests
    per 2s". Without OrderCount an external counter underestimates usage by 1-20x.
 2. Trading limits are per (User ID + Instrument ID), not globally per-UID.
    Each symbol has its own budget. Without Symbols the subscriber is forced to
    aggregate by endpoint and block one symbol because of load on another.
 3. The sub-account-level limit "1000 new+amend orders / 2s" (error 50061)
    counts only POST in Place and Amend categories, not Cancel or Query.
    Without Category the subscriber cannot model this dimension.

This type is the single official source of truth for all three dimensions.
The SDK populates it in the domain methods (swap/trading.go, swap/account.go),
where both instIds and the actual number of orders in the request body are known.

DEPENDENCIES:
None — this is a plain data struct.
*/

package okx

// RateLimitCategory — REST call classification from the OKX rate-limit model
// perspective. Used by external rate-limiters to distribute usage across
// different limit planes (per-endpoint, per-symbol, sub-account-level).
type RateLimitCategory string

const (
	// RateLimitCategoryPlace — order creation.
	// Endpoints: /api/v5/trade/order, /api/v5/trade/batch-orders,
	// /api/v5/trade/close-position.
	// Counted in sub-account 1000 new+amend orders / 2s (error 50061).
	RateLimitCategoryPlace RateLimitCategory = "place"

	// RateLimitCategoryAmend — order modification.
	// Endpoints: /api/v5/trade/amend-order, /api/v5/trade/amend-batch-orders.
	// Counted in sub-account 1000 new+amend orders / 2s.
	RateLimitCategoryAmend RateLimitCategory = "amend"

	// RateLimitCategoryCancel — order cancellation.
	// Endpoints: /api/v5/trade/cancel-order, /api/v5/trade/cancel-batch-orders,
	// /api/v5/trade/cancel-all-after.
	// NOT counted in sub-account 50061 (OKX counts only place + amend).
	RateLimitCategoryCancel RateLimitCategory = "cancel"

	// RateLimitCategoryQuery — private GET or non-trading POST (account
	// configuration). Per-UID, not included in sub-account 50061.
	// Endpoints: /api/v5/trade/orders-pending, /api/v5/account/*.
	RateLimitCategoryQuery RateLimitCategory = "query"

	// RateLimitCategoryMarketData — public GET (per-IP limits).
	// Endpoints: /api/v5/market/*, /api/v5/public/*.
	RateLimitCategoryMarketData RateLimitCategory = "market"

	// RateLimitCategoryUnknown — fallback for requests not covered by any
	// explicit category (e.g. internal health checks). The external
	// rate-limiter may either ignore such events or count them conservatively
	// as Query.
	RateLimitCategoryUnknown RateLimitCategory = ""
)

// String returns the string representation of the category.
func (c RateLimitCategory) String() string {
	return string(c)
}

// RateLimitEvent — structured rate-limit event that the SDK delivers to
// subscribers via okx.Config.RateLimitEventObserver.
//
// All fields are populated by the SDK strictly after a successful HTTP response
// from OKX (even if the response code != 0): the observer is called exactly
// once per completed REST call.
type RateLimitEvent struct {
	// Endpoint — request path (e.g. "/api/v5/trade/batch-orders").
	// Never empty. The same path appears in OKX docs §Rate Limits.
	Endpoint string

	// Method — HTTP request method in upper case (GET / POST / ...).
	Method string

	// Headers — rate-limit headers returned by OKX in the response:
	// ratelimit-limit / ratelimit-remaining / ratelimit-reset and their
	// x-ratelimit-* variants. As of v2.2.0 the OKX REST API does not return
	// these headers — the map will be empty but always non-nil. The SDK
	// already forwards them as soon as OKX starts sending them.
	Headers map[string]string

	// OrderCount — number of orders CREATED/AMENDED/CANCELLED by this request:
	//   - 1 for /trade/order, /trade/amend-order, /trade/cancel-order,
	//     /trade/close-position;
	//   - len(orders) for /trade/batch-orders, /trade/amend-batch-orders,
	//     /trade/cancel-batch-orders;
	//   - 0 for non-trading requests (account, market, public).
	//
	// This number expresses how many budget units OKX has charged/will charge
	// against the "300 orders per 2s" limit for the corresponding endpoint.
	// Use instead of request counter (as external rate-limiters used to):
	// on batches a request counter underestimates usage by 1-20x.
	OrderCount int

	// Symbols — sorted list of unique OKX InstIDs the request relates to:
	//   - 1 element for single trading methods (order InstID);
	//   - 1+ for batch (unique InstID set of all orders in the batch);
	//   - 1 for query methods with a required instId parameter
	//     (GetPositions, GetOpenOrders, GetSymbolInfo, ...);
	//   - empty ([]string{}, not nil) for general requests without instId
	//     (GetBalance without ccy, public/instruments list).
	//
	// OKX trading limits are per (UID + InstId), so the subscriber must
	// debit usage to the state of the corresponding symbols, not aggregate
	// by endpoint. Careful use of this field eliminates the
	// "one hot symbol blocks others" effect.
	Symbols []string

	// Category — classification by the OKX rate-limit model. Used by the
	// external rate-limiter for:
	//   - sub-account-level plane (Place + Amend = 1000 / 2s);
	//   - always-allow-Cancel policy;
	//   - correct window/limit selection for non-standard endpoints.
	Category RateLimitCategory
}
