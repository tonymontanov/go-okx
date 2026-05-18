/*
ФАЙЛ: types/system-status.go

ОПИСАНИЕ:
ServerTime — текущее время сервера OKX (для clock-sync).
SystemStatus — расписание maintenance-окон по продуктам биржи.

Маппятся из:
  - GET /api/v5/public/time   → ServerTime
  - GET /api/v5/system/status → SystemStatus (массив)

HFT-применение:
  - ServerTime: критично для clock skew detection — расхождение
    клиентских часов и серверных > N мс может стать причиной 401
    (invalid timestamp в signature);
  - SystemStatus: позволяет заранее остановить стратегию перед
    объявленным maintenance-окном и избежать «зависших» ордеров.
*/

package types

// ServerTime — текущее время сервера OKX.
type ServerTime struct {
	// Ts — таймштамп сервера в миллисекундах (ts).
	Ts int64
}

// SystemStatusState — состояние maintenance-окна.
type SystemStatusState string

const (
	// SystemStatusScheduled — окно запланировано (scheduled).
	SystemStatusScheduled SystemStatusState = "scheduled"
	// SystemStatusOngoing — окно сейчас активно (ongoing).
	SystemStatusOngoing SystemStatusState = "ongoing"
	// SystemStatusPreOpen — pre-open фаза после окна.
	SystemStatusPreOpen SystemStatusState = "pre_open"
	// SystemStatusCompleted — окно завершено (completed).
	SystemStatusCompleted SystemStatusState = "completed"
	// SystemStatusCanceled — окно отменено (canceled).
	SystemStatusCanceled SystemStatusState = "canceled"
)

// SystemStatus — одна запись о maintenance-окне.
type SystemStatus struct {
	// Title — описание (title).
	Title string
	// State — состояние окна (state).
	State SystemStatusState
	// BeginTs — начало в мс (begin).
	BeginTs int64
	// EndTs — конец в мс (end).
	EndTs int64
	// Href — ссылка на анонс (href).
	Href string
	// ServiceType — затронутый сервис (serviceType: "0" WS public,
	// "1" WS private, "2" WS account, "5" trading service, "6" block
	// trading, "8" trading service for batch orders, "99" general).
	ServiceType string
	// System — затронутая система (system: "classic"/"unified").
	System string
	// ScheduleDescription — детальное описание расписания (scheDesc).
	ScheduleDescription string
	// MaintType — тип ("1" scheduled, "2" unscheduled).
	MaintType string
	// PreOpenBegin — начало pre-open фазы в мс (preOpenBegin).
	PreOpenBegin int64
}

// SystemStatuses — слайс.
type SystemStatuses []SystemStatus
