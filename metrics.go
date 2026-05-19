/*
FILE: metrics.go

DESCRIPTION:
Public re-export of the metrics interface. The interface itself lives in
internal/okxmet so that any internal SDK package (ws, rest) can use it
without an import cycle. The root package re-exports the type/functions
via alias.

PROMETHEUS INTEGRATION:
A typical adapter in user code looks like this:

	type promFactory struct{ namespace string }
	func (p promFactory) Counter(name string, labels ...string) okx.Counter {
		var c prometheus.Counter = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: p.namespace, Name: name,
		})
		// labels can be forwarded to CounterVec.With(...).
		return c
	}

	cfg.Metrics = promFactory{namespace: "myapp"}

Since prometheus.Counter already has Inc/Add methods it implements
okx.Counter without any wrapper.
*/

package okx

import "github.com/tonymontanov/go-okx/v2/internal/okxmet"

// Counter — metrics counter. Alias.
type Counter = okxmet.Counter

// CounterFactory — counter factory. Alias.
type CounterFactory = okxmet.CounterFactory

// NoopMetrics returns a no-op counter factory. Used as the default.
func NoopMetrics() CounterFactory { return okxmet.Noop() }
