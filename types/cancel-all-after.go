/*
FILE: types/cancel-all-after.go

DESCRIPTION:
CancelAllAfterResult — OKX response to POST /api/v5/trade/cancel-all-after.

SEMANTICS (OKX dead-man's switch):
The endpoint arms a timer on the exchange side: if the client does not call
the endpoint again within `timeout` seconds, the exchange automatically cancels
ALL open orders for the account.

This is a risk-management safety mechanism: "if my process crashes / the
network drops / I freeze — let the exchange cancel everything I have open".

Usage modes:
  - timeout > 0 (10..120s): arm the timer. triggerTime in the response is the
    time when the exchange will cancel orders if it receives no refresh;
  - timeout == 0: disarm. triggerTime in the response will be empty.

HOW TO MAINTAIN THE TIMER:
Call CancelAllAfter every ~⅓ of timeout in the strategy hot loop
(e.g. every 10s for timeout=30s). This provides ample headroom for network delays.

PAIRING WITH MASS-CANCEL:
mass-cancel (Phase 2.1) — synchronous panic button "cancel everything now".
cancel-all-after — asynchronous "cancel everything in N seconds if I don't refresh".
Used together: arm on startup, mass-cancel or disarm on graceful shutdown.
*/

package types

// CancelAllAfterResult — exchange response to cancel-all-after.
type CancelAllAfterResult struct {
	// TriggerTimeMs — timestamp in ms when the exchange will apply cancel-all
	// if no refresh is received. 0 for disarm (timeout=0).
	TriggerTimeMs int64
	// TsMs — server response timestamp in ms.
	TsMs int64
}
