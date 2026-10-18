package cron

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestAddValidatesSpec(t *testing.T) {
	is := is.New(t)
	s := New(nil, Config{Name: "test"})

	is.NoErr(s.Add("@every 1s", "ok", func(context.Context) error { return nil }))         // @every descriptor
	is.NoErr(s.Add("0 3 * * *", "daily", func(context.Context) error { return nil }))      // 5-field standard
	is.True(s.Add("not a spec", "bad", func(context.Context) error { return nil }) != nil) // rejected
	is.True(s.Add("@every 1s", "nil", nil) != nil)                                         // nil job rejected
}

func TestSchedulerRunsAndStops(t *testing.T) {
	is := is.New(t)
	s := New(nil, Config{Name: "test"})

	var runs atomic.Int32
	// @every rounds sub-second delays up to 1s (robfig ConstantDelaySchedule), so
	// the smallest useful cadence is 1s; a ~2.2s window yields at least two fires.
	is.NoErr(s.Add("@every 1s", "count", func(context.Context) error {
		runs.Add(1)
		return nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	select {
	case err := <-done:
		is.NoErr(err) // Start returns nil on graceful ctx cancellation
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop after context cancellation")
	}
	is.True(runs.Load() >= 2) // fired at least twice in ~350ms at 100ms cadence
}

func TestSchedulerRecoversPanic(t *testing.T) {
	is := is.New(t)
	var after, panics atomic.Int32
	s := New(nil, Config{Name: "test"}, WithOnPanic(func(string, string) {
		panics.Add(1)
	}))

	is.NoErr(s.Add("@every 1s", "panicky", func(context.Context) error {
		after.Add(1)
		panic("boom") // must be recovered, not crash the process/scheduler
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()
	is.NoErr(s.Start(ctx))

	is.True(after.Load() >= 2)            // kept firing despite each run panicking
	is.Equal(panics.Load(), after.Load()) // the handler sees every panic
}

// Every run is counted under its job name by result, a panic too.
func TestSchedulerRecordsRuns(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	s := New(nil, Config{Name: "test", Metrics: reg})
	ends := []func() error{
		func() error { return nil },
		func() error { return errors.New("boom") },
		func() error { panic("boom") },
	}
	is.NoErr(s.Add("@daily", "cleanup", func(context.Context) error {
		end := ends[0]
		ends = ends[1:]
		return end()
	}))

	for range 3 {
		s.run(t.Context(), s.entries[0])
	}

	want := `
# HELP skit_cron_runs_total Runs by cron_job and result: ok, error or panic.
# TYPE skit_cron_runs_total counter
skit_cron_runs_total{cron_job="cleanup",result="error"} 1
skit_cron_runs_total{cron_job="cleanup",result="ok"} 1
skit_cron_runs_total{cron_job="cleanup",result="panic"} 1
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_cron_runs_total"))
}
