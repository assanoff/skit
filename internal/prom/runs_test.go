package prom

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// newTestRuns builds the Runs of the unit name, named skit_test_ticks_total
// and so on.
func newTestRuns(reg prometheus.Registerer, name string) *Runs {
	return NewRuns(reg, RunsOpts{Subsystem: "test", Run: "tick", Label: "worker"}, name)
}

func TestRunsRecord(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	r := newTestRuns(reg, "sync")

	r.Record(t.Context(), time.Millisecond, true, nil)
	r.Record(t.Context(), time.Millisecond, true, errors.New("boom"))
	r.Record(t.Context(), time.Millisecond, false, nil)

	want := `
# HELP skit_test_ticks_total Runs by worker and result: ok, error or panic.
# TYPE skit_test_ticks_total counter
skit_test_ticks_total{result="error",worker="sync"} 1
skit_test_ticks_total{result="ok",worker="sync"} 1
skit_test_ticks_total{result="panic",worker="sync"} 1
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_test_ticks_total"))
	n, err := testutil.GatherAndCount(reg, "skit_test_tick_duration_seconds")
	is.NoErr(err)
	is.Equal(n, 1)
	last := testutil.ToFloat64(r.lastSuccess.WithLabelValues("sync"))
	is.True(last > 0) // set by the successful run
}

// Before the first run the counters are 0 and there is no last success.
func TestRunsBeforeFirstRun(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	newTestRuns(reg, "sync")

	n, err := testutil.GatherAndCount(reg, "skit_test_ticks_total")
	is.NoErr(err)
	is.Equal(n, 3) // ok, error and panic at 0
	n, err = testutil.GatherAndCount(reg, "skit_test_last_success_timestamp_seconds")
	is.NoErr(err)
	is.Equal(n, 0)
}

// Two units on one registry share the metrics, each under its own name.
func TestRunsSharedRegistry(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	newTestRuns(reg, "a").Record(t.Context(), time.Millisecond, true, nil)
	newTestRuns(reg, "b").Record(t.Context(), time.Millisecond, true, nil)

	n, err := testutil.GatherAndCount(reg, "skit_test_last_success_timestamp_seconds")
	is.NoErr(err)
	is.Equal(n, 2)
}
