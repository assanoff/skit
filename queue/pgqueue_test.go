package queue_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/safetick"
	"github.com/assanoff/skit/worker"
)

// newPG starts Postgres for the test and returns a queue on it with its
// schema.
func newPG(t *testing.T) *queue.PG {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test requires docker")
	}
	pg := dbtest.NewPostgres(t.Context(), t, dbtest.Config{})
	q := queue.NewPG(slog.New(slog.DiscardHandler), pg.DB, queue.Options{})
	if err := q.EnsureSchema(t.Context()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return q
}

// schedule enqueues a task of kind under name, due after delay.
func schedule(t *testing.T, q *queue.PG, name, kind string, delay time.Duration) {
	t.Helper()
	p := queue.ScheduleParams{Name: name, Kind: kind, Delay: delay}
	if _, err := q.Schedule(t.Context(), p); err != nil {
		t.Fatalf("schedule %s: %v", name, err)
	}
}

// names returns the names of tasks in order.
func names(tasks []queue.Task) []string {
	out := make([]string, len(tasks))
	for i := range tasks {
		out[i] = tasks[i].Name
	}
	return out
}

// Stats counts the ready, the leased and the dead tasks; a delayed task is in
// none of them.
func TestPGStats(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	q := queue.NewPG(slog.New(slog.DiscardHandler), pg.DB, queue.Options{})
	is.NoErr(q.EnsureSchema(ctx))

	for _, delay := range []time.Duration{0, 0, 0, time.Hour} {
		_, err := q.Schedule(ctx, queue.ScheduleParams{Kind: "email", Delay: delay})
		is.NoErr(err)
	}
	now := time.Now().UTC()
	claimed, err := q.Claim(ctx, now, 2)
	is.NoErr(err)
	is.Equal(len(claimed), 2) // two of the three ready tasks are leased
	is.NoErr(q.MarkFailed(ctx, claimed[0], "boom", true, now))

	st, err := q.Stats(ctx, now)
	is.NoErr(err)
	is.Equal(st, queue.Stats{Pending: 1, Leased: 1, Dead: 1})
}

// Claim returns its batch in run_at order, and Release gives back the tasks of
// a batch with their attempt, guarded by the lease.
func TestPGClaimOrderAndRelease(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)
	// Scheduled first, due last: the storage order is the reverse of run_at.
	schedule(t, q, "a", "job", 30*time.Millisecond)
	schedule(t, q, "b", "job", 20*time.Millisecond)
	schedule(t, q, "c", "job", 10*time.Millisecond)
	later := time.Now().UTC().Add(time.Hour)

	claimed, err := q.Claim(ctx, later, 10)
	is.NoErr(err)
	is.Equal(names(claimed), []string{"c", "b", "a"}) // the batch comes in run_at order
	is.NoErr(q.MarkDone(ctx, claimed[0], later))
	is.NoErr(q.Release(ctx, claimed[1:]))

	again, err := q.Claim(ctx, later, 10)
	is.NoErr(err)
	is.Equal(names(again), []string{"b", "a"}) // released tasks are claimable at once
	is.Equal(again[0].Attempts, 1)             // with the attempt of the release taken back

	is.NoErr(q.Release(ctx, claimed[1:])) // under a lease since replaced
	st, err := q.Stats(ctx, later)
	is.NoErr(err)
	is.Equal(st.Leased, int64(2)) // a stale lease releases nothing
}

// A panicked handler is retried by default; OnPanic can dead-letter the task
// instead.
func TestPGProcessorPanic(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)
	mux := queue.NewMux()
	is.NoErr(mux.Register("retry", func(context.Context, queue.Task) error {
		panic("boom")
	}))
	is.NoErr(mux.Register("drop", func(context.Context, queue.Task) error {
		panic("bad payload")
	}))

	schedule(t, q, "r", "retry", 0)
	byDefault := worker.NewProcessor[queue.Task](nil, q, mux, q, worker.ProcessorConfig{})
	is.NoErr(byDefault.Tick()(ctx))
	st, err := q.Stats(ctx, time.Now().UTC())
	is.NoErr(err)
	is.Equal(st, queue.Stats{}) // the task waits for its retry: neither ready, leased nor dead

	errDead := errors.New("dead")
	schedule(t, q, "d", "drop", 0)
	decided := worker.NewProcessor[queue.Task](nil, q, mux, q, worker.ProcessorConfig{
		IsTerminal: func(err error) bool { return errors.Is(err, errDead) },
		OnPanic: func(context.Context, any, *safetick.PanicError) error {
			return errDead
		},
	})
	is.NoErr(decided.Tick()(ctx))
	st, err = q.Stats(ctx, time.Now().UTC())
	is.NoErr(err)
	is.Equal(st, queue.Stats{Dead: 1}) // OnPanic dead-letters the task

	retried, err := q.Claim(ctx, time.Now().UTC().Add(time.Hour), 10)
	is.NoErr(err)
	is.Equal(names(retried), []string{"r"})       // the default retried the task
	is.Equal(retried[0].LastError, "panic: boom") // with the panic as its error
	is.Equal(retried[0].Attempts, 2)
}

// A shutdown mid-batch acknowledges the task being handled and releases the
// rest untouched.
func TestPGProcessorShutdown(t *testing.T) {
	is := is.New(t)
	q := newPG(t)
	for _, name := range []string{"a", "b", "c"} {
		schedule(t, q, name, "job", 0)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var handled []string
	h := worker.HandlerFunc[queue.Task](func(_ context.Context, task queue.Task) error {
		handled = append(handled, task.Name)
		cancel() // shutdown while the task is handled
		return nil
	})
	p := worker.NewProcessor[queue.Task](nil, q, h, q, worker.ProcessorConfig{})

	is.NoErr(p.Tick()(ctx))
	is.Equal(handled, []string{"a"}) // no task starts after the cancel

	rest, err := q.Claim(t.Context(), time.Now().UTC(), 10)
	is.NoErr(err)
	is.Equal(names(rest), []string{"b", "c"}) // the handled task is done, the rest is back
	is.Equal(rest[0].Attempts, 1)             // with no attempt counted
}
