package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func newCounter() prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{Name: "test_total", Help: "h"})
}

func TestRegisterReturnsExistingOnDuplicate(t *testing.T) {
	reg := prometheus.NewRegistry()

	c1 := Register(reg, newCounter())
	c2 := Register(reg, newCounter()) // identical name → must reuse, not panic

	c1.Inc()
	c2.Inc()
	// Same underlying collector: both Inc calls land on one series.
	if c1 != c2 {
		t.Error("Register did not return the already-registered collector")
	}
}

func TestRegisterPanicsOnConflict(t *testing.T) {
	reg := prometheus.NewRegistry()
	Register(reg, newCounter())

	defer func() {
		if recover() == nil {
			t.Error("expected panic when a different collector clashes on the name")
		}
	}()
	// Same name, different type (gauge vs counter) → genuine conflict.
	Register(reg, prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_total", Help: "h"}))
}

func TestRegisterNilRegistryIsNoop(t *testing.T) {
	c := newCounter()
	if got := Register(nil, c); got != c {
		t.Error("nil registry should return the collector unchanged")
	}
	c.Inc() // must not panic
}
