/*
ФАЙЛ: metrics.go

ОПИСАНИЕ:
Публичный реэкспорт интерфейса метрик. Сам интерфейс живёт в internal/okxmet,
чтобы любой internal-пакет SDK (ws, rest) мог использовать его без
import-cycle. Корневой пакет переэкспортирует тип/функции через alias.

ПОДКЛЮЧЕНИЕ PROMETHEUS:
В коде пользователя адаптер выглядит примерно так:

	type promFactory struct{ namespace string }
	func (p promFactory) Counter(name string, labels ...string) okx.Counter {
		var c prometheus.Counter = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: p.namespace, Name: name,
		})
		// labels можно прокинуть в CounterVec.With(...).
		return c
	}

	cfg.Metrics = promFactory{namespace: "myapp"}

Поскольку prometheus.Counter уже имеет методы Inc/Add — он реализует
интерфейс okx.Counter без обёртки.
*/

package okx

import "github.com/tonymontanov/go-okx/v2/internal/okxmet"

// Counter — счётчик метрик. Alias.
type Counter = okxmet.Counter

// CounterFactory — фабрика счётчиков. Alias.
type CounterFactory = okxmet.CounterFactory

// NoopMetrics возвращает no-op фабрику метрик. Используется как default.
func NoopMetrics() CounterFactory { return okxmet.Noop() }
