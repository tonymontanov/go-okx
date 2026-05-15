/*
ФАЙЛ: internal/okxmet/metrics.go

ОПИСАНИЕ:
Минимальный интерфейс счётчиков и фабрики счётчиков для SDK. По форме
совпадает с prometheus.Counter (Inc/Add), но не привязан к Prometheus —
пользователь подключает любой бэкенд (Prometheus, OpenTelemetry, statsd)
тонким адаптером 5–10 строк.

ОСНОВНЫЕ СУЩНОСТИ:
  - Counter:        интерфейс счётчика. Inc() и Add(float64).
  - CounterFactory: фабрика, возвращающая Counter по имени + label-парам
                    (k1, v1, k2, v2 ...). Lable-парами сделаны для двух целей:
                      1. совместимость с prometheus.NewCounterVec.WithLabelValues;
                      2. отсутствие аллокации map[string]string на каждый
                         вызов в hot-path.
  - Noop:           реализация по умолчанию, все вызовы — no-op.

ИМЕНОВАНИЕ СЧЁТЧИКОВ В SDK:
SDK сам решает, какие counters создавать и под какими именами. Имена
выбраны стабильные/предсказуемые, чтобы пользовательский bench/grafana
работали без переписывания:
  okx_ws_messages_received_total
  okx_ws_messages_dropped_total
  okx_ws_reconnects_total
  okx_ws_subscriptions_total
  okx_ws_ping_failed_total
*/

package okxmet

// Counter — единичный counter (монотонно растущее число).
type Counter interface {
	// Inc увеличивает значение на 1.
	Inc()
	// Add увеличивает значение на delta. Если delta < 0 — реализация может
	// проигнорировать (counter монотонный).
	Add(delta float64)
}

// CounterFactory — фабрика counters. labels — k1, v1, k2, v2, ... .
type CounterFactory interface {
	// Counter возвращает счётчик по имени и labels. Реализация должна
	// гарантировать, что один и тот же набор (name, labels) возвращает
	// одну и ту же запись (например, через sync.Map).
	Counter(name string, labels ...string) Counter
}

// Noop — реализация по умолчанию.
type noopFactory struct{}
type noopCounter struct{}

// Noop возвращает no-op фабрику (singleton).
func Noop() CounterFactory { return noopFactorySingleton }

var (
	noopFactorySingleton CounterFactory = noopFactory{}
	noopCounterSingleton Counter        = noopCounter{}
)

func (noopFactory) Counter(string, ...string) Counter { return noopCounterSingleton }

func (noopCounter) Inc()         {}
func (noopCounter) Add(float64) {}
