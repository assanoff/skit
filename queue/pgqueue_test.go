package queue

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/retry"
	"github.com/assanoff/skit/safetick"
)

// newPG starts Postgres for the test and returns a store on it with its
// schema.
func newPG(t *testing.T) *PG {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test requires docker")
	}
	pg := dbtest.NewPostgres(t.Context(), t, dbtest.Config{})
	q := NewPG(slog.New(slog.DiscardHandler), pg.DB)
	if err := q.EnsureSchema(t.Context()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return q
}

// schedule enqueues p and fails the test on an error.
func schedule(t *testing.T, q *PG, p Params) {
	t.Helper()
	if _, err := q.Schedule(t.Context(), p); err != nil {
		t.Fatalf("schedule %s/%s: %v", p.Queue, p.Kind, err)
	}
}

// kinds returns the kinds of tasks in order.
func kinds(tasks []leased) []string {
	out := make([]string, len(tasks))
	for i := range tasks {
		out[i] = tasks[i].Kind
	}
	return out
}

// later is a claim time past every delay and backoff of a test.
func later() time.Time {
	return time.Now().UTC().Add(time.Hour)
}

// A Key deduplicates within its queue only; no Key schedules every call.
func TestPGScheduleKey(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)

	for _, c := range []struct {
		p    Params
		want bool
	}{
		{Params{Queue: "a", Kind: "k", Key: "x"}, true},
		{Params{Queue: "a", Kind: "k", Key: "x"}, false}, // same key, same queue
		{Params{Queue: "b", Kind: "k", Key: "x"}, true},  // same key, another queue
		{Params{Queue: "a", Kind: "k"}, true},
		{Params{Queue: "a", Kind: "k"}, true}, // no key, no dedup
	} {
		inserted, err := q.Schedule(ctx, c.p)
		is.NoErr(err)
		is.Equal(inserted, c.want)
	}

	_, err := q.Schedule(ctx, Params{Kind: "k"})
	is.True(err != nil) // a queue is required
	_, err = q.Schedule(ctx, Params{Queue: "a"})
	is.True(err != nil) // and so is a kind
}

// A claim takes the tasks of its queue only, in run_at order; an expired
// lease is reclaimed, and release gives back the tasks of its lease only.
func TestPGClaimAndRelease(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)
	// Scheduled first, due last: the storage order is the reverse of run_at.
	schedule(t, q, Params{Queue: "a", Kind: "3", Delay: 30 * time.Millisecond})
	schedule(t, q, Params{Queue: "a", Kind: "2", Delay: 20 * time.Millisecond})
	schedule(t, q, Params{Queue: "a", Kind: "1", Delay: 10 * time.Millisecond})
	schedule(t, q, Params{Queue: "b", Kind: "other"})
	now := later()

	claimed, err := q.claim(ctx, "a", now, time.Minute, 10)
	is.NoErr(err)
	is.Equal(kinds(claimed), []string{"1", "2", "3"}) // its queue only, in run_at order
	is.Equal(claimed[0].Queue, "a")

	none, err := q.claim(ctx, "a", now, time.Minute, 10)
	is.NoErr(err)
	is.Equal(len(none), 0) // leased tasks are not claimed again

	reclaimed, err := q.claim(ctx, "a", now.Add(2*time.Minute), time.Minute, 1)
	is.NoErr(err)
	is.Equal(kinds(reclaimed), []string{"1"}) // an expired lease is reclaimed

	is.NoErr(q.release(ctx, claimed)) // the first task is under another lease now
	again, err := q.claim(ctx, "a", now, time.Minute, 10)
	is.NoErr(err)
	is.Equal(kinds(again), []string{"2", "3"}) // the rest of the lease is back
	is.Equal(again[0].Attempts, 0)             // with no attempt counted
}

// A failed task counts its attempt and runs again at its time; a dead one
// stays for the cleaner, which removes the old dead tasks of its queue only.
func TestPGMarksStatsCleanup(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)
	for _, k := range []string{"done", "retry", "dead"} {
		schedule(t, q, Params{Queue: "a", Kind: k})
	}
	schedule(t, q, Params{Queue: "b", Kind: "dead"})
	now := time.Now().UTC()

	claimed, err := q.claim(ctx, "a", now, time.Minute, 10)
	is.NoErr(err)
	is.NoErr(q.markDone(ctx, claimed[0]))
	is.NoErr(q.markFailed(ctx, claimed[1], "down", false, now, now.Add(time.Minute)))
	is.NoErr(q.markFailed(ctx, claimed[2], "bad", true, now, now))
	is.True(errors.Is(q.markDone(ctx, claimed[0]), errLeaseLost)) // the task is gone
	other, err := q.claim(ctx, "b", now, time.Minute, 10)
	is.NoErr(err)
	is.NoErr(q.markFailed(ctx, other[0], "bad", true, now, now))

	st, err := q.stats(ctx, "a", now)
	is.NoErr(err)
	is.Equal(st, stats{Dead: 1}) // the retried task waits for its time

	retried, err := q.claim(ctx, "a", now.Add(time.Minute), time.Minute, 10)
	is.NoErr(err)
	is.Equal(kinds(retried), []string{"retry"})
	is.Equal(retried[0].Attempts, 1)
	is.Equal(retried[0].LastError, "down")

	n, err := q.cleanup(ctx, "a", now.Add(time.Second))
	is.NoErr(err)
	is.Equal(n, int64(1)) // the dead task of its queue only
	st, err = q.stats(ctx, "b", now)
	is.NoErr(err)
	is.Equal(st.Dead, int64(1))
}

// tick runs one claim of the processor of cfg over h.
func tick(ctx context.Context, t *testing.T, q *PG, cfg Config, h JobFunc, opts ...Option) {
	t.Helper()
	p := newProcessor(nil, q, cfg.withDefaults(), h, newOptions(opts))
	if err := p.Tick()(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

// The error of the handler decides the outcome of each task of a batch.
func TestProcessorOutcomes(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)
	for _, k := range []string{"ok", "retry", "dead", "unknown"} {
		schedule(t, q, Params{Queue: "a", Kind: k})
	}
	mux := NewMux()
	is.NoErr(mux.Register("ok", func(context.Context, Task) error {
		return nil
	}))
	is.NoErr(mux.Register("retry", func(context.Context, Task) error {
		return errors.New("down")
	}))
	is.NoErr(mux.Register("dead", func(context.Context, Task) error {
		return Permanent(errors.New("bad"))
	}))

	tick(ctx, t, q, Config{Queue: "a", Batch: 10}, mux.Handle)

	st, err := q.stats(ctx, "a", time.Now().UTC())
	is.NoErr(err)
	is.Equal(st, stats{Dead: 2}) // ok is removed, retry waits for its backoff
	retried, err := q.claim(ctx, "a", later(), time.Minute, 10)
	is.NoErr(err)
	is.Equal(kinds(retried), []string{"retry"})
	is.Equal(retried[0].Attempts, 1)
	is.True(retried[0].RunAt.After(retried[0].CreatedAt.Add(time.Second))) // after a backoff of 2s
}

// Halt retries its task and stops the batch: the rest goes back untouched.
func TestProcessorHalt(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)
	for _, k := range []string{"a", "b", "c"} {
		schedule(t, q, Params{Queue: "events", Kind: k})
	}
	var handled []string
	h := func(_ context.Context, t Task) error {
		handled = append(handled, t.Kind)
		return Halt(errors.New("broker down"))
	}

	tick(ctx, t, q, Config{Queue: "events", Batch: 10}, h)

	is.Equal(handled, []string{"a"}) // the batch stops at Halt
	rest, err := q.claim(ctx, "events", time.Now().UTC(), time.Minute, 10)
	is.NoErr(err)
	is.Equal(kinds(rest), []string{"b", "c"}) // the rest is back at once
	is.Equal(rest[0].Attempts, 0)             // with no attempt counted
}

// With MaxAttempts a task dies at its last failed attempt.
func TestProcessorMaxAttempts(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)
	schedule(t, q, Params{Queue: "a", Kind: "k"})
	fail := func(context.Context, Task) error { return errors.New("down") }
	cfg := Config{Queue: "a"}
	backoff := WithBackoff(retry.Backoff{Base: time.Nanosecond, MaxAttempts: 2})

	tick(ctx, t, q, cfg, fail, backoff)
	tick(ctx, t, q, cfg, fail, backoff)

	st, err := q.stats(ctx, "a", time.Now().UTC())
	is.NoErr(err)
	is.Equal(st, stats{Dead: 1}) // the second failure is the last
}

// A panic retries its task by default; WithOnPanic decides otherwise.
func TestProcessorPanic(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	q := newPG(t)
	boom := func(context.Context, Task) error { panic("boom") }

	schedule(t, q, Params{Queue: "a", Kind: "k"})
	tick(ctx, t, q, Config{Queue: "a"}, boom)
	retried, err := q.claim(ctx, "a", later(), time.Minute, 10)
	is.NoErr(err)
	is.Equal(len(retried), 1)                     // the default retries the task
	is.Equal(retried[0].LastError, "panic: boom") // with the panic as its error

	var seen Task
	schedule(t, q, Params{Queue: "b", Kind: "k"})
	tick(ctx, t, q, Config{Queue: "b"}, boom,
		WithOnPanic(func(_ context.Context, t Task, p *safetick.PanicError) error {
			seen = t
			return Permanent(p)
		}))
	is.Equal(seen.Queue, "b") // WithOnPanic gets the task
	st, err := q.stats(ctx, "b", time.Now().UTC())
	is.NoErr(err)
	is.Equal(st, stats{Dead: 1}) // and decides its outcome
}

// A shutdown mid-batch removes the task being handled and gives back the
// rest untouched.
func TestProcessorShutdown(t *testing.T) {
	is := is.New(t)
	q := newPG(t)
	for _, k := range []string{"a", "b", "c"} {
		schedule(t, q, Params{Queue: "q", Kind: k})
	}
	ctx, cancel := context.WithCancel(t.Context())
	var handled []string
	h := func(_ context.Context, t Task) error {
		handled = append(handled, t.Kind)
		cancel() // shutdown while the task is handled
		return nil
	}

	tick(ctx, t, q, Config{Queue: "q", Batch: 10}, h)

	is.Equal(handled, []string{"a"}) // no task starts after the cancel
	rest, err := q.claim(t.Context(), "q", time.Now().UTC(), time.Minute, 10)
	is.NoErr(err)
	is.Equal(kinds(rest), []string{"b", "c"}) // the handled task is gone, the rest is back
}
