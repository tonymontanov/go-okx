/*
FILE: types/system-status.go

DESCRIPTION:
ServerTime — current OKX server time (for clock sync).
SystemStatus — schedule of maintenance windows per exchange product.

Mapped from:
  - GET /api/v5/public/time   → ServerTime
  - GET /api/v5/system/status → SystemStatus (array)

HFT usage:
  - ServerTime: critical for clock skew detection — a divergence between
    client and server clocks > N ms can cause 401 (invalid timestamp in
    the signature);
  - SystemStatus: allows stopping a strategy ahead of an announced
    maintenance window to avoid "stuck" orders.
*/

package types

// ServerTime — current OKX server time.
type ServerTime struct {
	// Ts — server timestamp in milliseconds (ts).
	Ts int64
}

// SystemStatusState — maintenance window state.
type SystemStatusState string

const (
	// SystemStatusScheduled — window is scheduled (scheduled).
	SystemStatusScheduled SystemStatusState = "scheduled"
	// SystemStatusOngoing — window is currently active (ongoing).
	SystemStatusOngoing SystemStatusState = "ongoing"
	// SystemStatusPreOpen — pre-open phase after the window.
	SystemStatusPreOpen SystemStatusState = "pre_open"
	// SystemStatusCompleted — window has completed (completed).
	SystemStatusCompleted SystemStatusState = "completed"
	// SystemStatusCanceled — window was canceled (canceled).
	SystemStatusCanceled SystemStatusState = "canceled"
)

// SystemStatus — one maintenance window record.
type SystemStatus struct {
	// Title — description (title).
	Title string
	// State — window state (state).
	State SystemStatusState
	// BeginTs — start in ms (begin).
	BeginTs int64
	// EndTs — end in ms (end).
	EndTs int64
	// Href — link to the announcement (href).
	Href string
	// ServiceType — affected service (serviceType: "0" WS public,
	// "1" WS private, "2" WS account, "5" trading service, "6" block
	// trading, "8" trading service for batch orders, "99" general).
	ServiceType string
	// System — affected system (system: "classic"/"unified").
	System string
	// ScheduleDescription — detailed schedule description (scheDesc).
	ScheduleDescription string
	// MaintType — type ("1" scheduled, "2" unscheduled).
	MaintType string
	// PreOpenBegin — pre-open phase start in ms (preOpenBegin).
	PreOpenBegin int64
}

// SystemStatuses — slice.
type SystemStatuses []SystemStatus
