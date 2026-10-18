package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// sequence returns a tick whose calls end, in turn, as ends do.
func sequence(ends ...func() error) TickFunc {
	return func(context.Context) error {
		end := ends[0]
		ends = ends[1:]
		return end()
	}
}

func succeed() error { return nil }
func fail() error    { return errors.New("boom") }
func crash() error   { panic("boom") }

const ticksWant = `
# HELP skit_worker_ticks_total Runs by worker and result: ok, error or panic.
# TYPE skit_worker_ticks_total counter
skit_worker_ticks_total{result="error",worker="sync"} 1
skit_worker_ticks_total{result="ok",worker="sync"} 1
skit_worker_ticks_total{result="panic",worker="sync"} 1
`

// Every tick is counted by result; a panic is recovered, counted and handed
// to the WithOnPanic handler.
func TestLoopRecordsTicks(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	var panics []string
	l := NewLoop(nil, LoopConfig{Name: "sync", Metrics: reg},
		sequence(succeed, fail, crash),
		WithOnPanic(func(worker, _ string) { panics = append(panics, worker) }))

	for range 3 {
		l.runTick(t.Context())
	}

	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(ticksWant), "skit_worker_ticks_total"))
	is.Equal(panics, []string{"sync"})
	n, err := testutil.GatherAndCount(reg, "skit_worker_last_success_timestamp_seconds")
	is.NoErr(err)
	is.Equal(n, 1)
}

// An adaptive tick is counted the same way; one that panicked is Idle.
func TestPacedLoopRecordsTicks(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	ends := sequence(succeed, fail, crash)
	l := NewPacedLoop(nil, LoopConfig{Name: "sync", Metrics: reg},
		func(ctx context.Context) (Pace, error) {
			return Busy, ends(ctx)
		})

	is.Equal(l.runPacedTick(t.Context()), Busy)
	is.Equal(l.runPacedTick(t.Context()), Busy)
	is.Equal(l.runPacedTick(t.Context()), Idle) // the tick panicked

	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(ticksWant), "skit_worker_ticks_total"))
}

func TestLoopDefaultName(t *testing.T) {
	l := NewLoop(nil, LoopConfig{}, sequence())

	is.New(t).Equal(l.Name(), "worker")
}
