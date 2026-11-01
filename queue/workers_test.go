package queue_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

// names returns the names of runnables in order.
func names(rs []worker.Runnable) []string {
	out := make([]string, len(rs))
	for i := range rs {
		out[i] = rs[i].Name()
	}
	return out
}

func ignore(context.Context, queue.Task) error { return nil }

func TestWorkers(t *testing.T) {
	is := is.New(t)

	_, err := queue.Workers(nil, &queue.PG{}, queue.Config{}, ignore)
	is.True(err != nil) // a queue is required
	_, err = queue.Workers(nil, &queue.PG{}, queue.Config{Queue: "q"}, nil)
	is.True(err != nil) // and so is a handler

	rs, err := queue.Workers(nil, &queue.PG{}, queue.Config{Queue: "q", Concurrency: 2}, ignore)
	is.NoErr(err)
	is.Equal(names(rs), []string{"queue-q", "queue-q", "queue-q-cleaner"})

	rs, err = queue.Workers(nil, &queue.PG{}, queue.Config{Queue: "q"}, ignore,
		queue.WithMetrics(prometheus.NewRegistry()))
	is.NoErr(err)
	is.Equal(names(rs), []string{"queue-q", "queue-q-cleaner", "queue-q-backlog"})
}

// The workers of a queue handle its tasks and remove them; the tasks of
// another queue stay.
func TestWorkersRun(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	q := queue.NewPG(slog.New(slog.DiscardHandler), pg.DB)
	is.NoErr(q.EnsureSchema(ctx))
	for _, p := range []queue.Params{
		{Queue: "email", Kind: "welcome"},
		{Queue: "email", Kind: "welcome"},
		{Queue: "bi", Kind: "export"},
	} {
		_, err := q.Schedule(ctx, p)
		is.NoErr(err)
	}

	handled := make(chan string, 3)
	h := func(_ context.Context, t queue.Task) error {
		handled <- t.Queue
		return nil
	}
	cfg := queue.Config{Queue: "email", Poll: 10 * time.Millisecond}
	rs, err := queue.Workers(nil, q, cfg, h)
	is.NoErr(err)
	g := worker.NewGroup(nil, worker.GroupConfig{ShutdownTimeout: time.Second})
	for _, r := range rs {
		g.Add(r)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- g.Run(runCtx) }()

	is.Equal(<-handled, "email")
	is.Equal(<-handled, "email")
	cancel()
	<-done

	left, err := dbx.QueryList[string](ctx, pg.DB, `SELECT queue FROM queue_tasks`)
	is.NoErr(err)
	is.Equal(left, []string{"bi"}) // handled tasks are removed, another queue is untouched
	is.Equal(len(handled), 0)
}
