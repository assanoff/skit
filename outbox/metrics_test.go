package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRelayMetrics(t *testing.T) {
	is := is.New(t)
	m := newRelayMetrics(prometheus.NewRegistry())

	m.observe(t.Context(), 5*time.Millisecond, nil)
	m.observe(t.Context(), time.Second, context.DeadlineExceeded)

	is.Equal(testutil.ToFloat64(m.published), 1.0)
	is.Equal(testutil.ToFloat64(m.failed), 1.0)
	is.Equal(testutil.CollectAndCount(m.duration), 1) // a failed publish has no duration
}

// Two relays on one registry share the metrics instead of failing to register.
func TestRelayMetricsSharedRegistry(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	first := newRelayMetrics(reg)
	second := newRelayMetrics(reg)

	second.observe(t.Context(), time.Millisecond, nil)

	is.Equal(testutil.ToFloat64(first.published), 1.0)
}

// With a nil registry the metrics are recorded but not registered.
func TestRelayMetricsNilRegistry(t *testing.T) {
	is := is.New(t)
	m := newRelayMetrics(nil)

	m.observe(t.Context(), time.Millisecond, nil)

	is.Equal(testutil.ToFloat64(m.published), 1.0)
}

func TestAddRows(t *testing.T) {
	is := is.New(t)
	c := newCounter(nil, "test_total", "Test rows.")

	addRows(c, 3)
	addRows(c, 0)
	addRows(c, -1) // a negative count must not panic the counter

	is.Equal(testutil.ToFloat64(c), 3.0)
}
