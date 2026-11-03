package auditqueue_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditpg"
	"github.com/assanoff/skit/auditlog/auditqueue"
	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

// The durable recorder: Record schedules the entry on the audit queue, and the
// workers of the queue write it through the core until a signal. The example
// needs Postgres, so go test builds it but does not run it.
func Example() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.Default()

	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()

	store := auditpg.NewStore(log, db)
	q := queue.NewPG(log, db)
	if err := errors.Join(store.EnsureSchema(ctx), q.EnsureSchema(ctx)); err != nil {
		log.ErrorContext(ctx, "schema", "err", err)
		return
	}
	core := auditlog.NewCore(log, store)

	// Where the model changes: one INSERT into the queue.
	rec := auditqueue.NewRecorder(q, log)
	rec.Record(ctx, auditlog.NewAuditLog{
		ModelType: "widget", ModelID: "w-1", CreatedBy: "ann",
		Payload: map[string]string{"name": "gear"},
	})

	workers, err := auditqueue.NewWorkers(q, core, log, auditqueue.WorkerConfig{
		Metrics: metrics.New(),
	})
	if err != nil {
		log.ErrorContext(ctx, "workers", "err", err)
		return
	}
	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	g.Add(workers...)
	if err := g.Run(ctx); err != nil {
		log.ErrorContext(ctx, "group stopped", "err", err)
	}
}

// enqueuerFunc is an auditqueue.Enqueuer of a function, in place of the queue.
type enqueuerFunc func(ctx context.Context, p queue.Params) (bool, error)

func (f enqueuerFunc) Schedule(ctx context.Context, p queue.Params) (bool, error) {
	return f(ctx, p)
}

// Record schedules the entry as a task of the audit queue, an auditbus.Event
// in its payload. A failed schedule is logged, not returned: recording is
// best-effort.
func ExampleRecorder() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	ctx := context.Background()
	entry := auditlog.NewAuditLog{
		ModelType: "widget", ModelID: "w-1", CreatedBy: "ann",
		Payload: map[string]string{"name": "gear"},
	}

	var q enqueuerFunc = func(_ context.Context, p queue.Params) (bool, error) {
		fmt.Println(p.Queue, p.Kind, string(p.Payload))
		return true, nil
	}
	auditqueue.NewRecorder(q, log).Record(ctx, entry)

	q = func(context.Context, queue.Params) (bool, error) {
		return false, errors.New("connection refused")
	}
	auditqueue.NewRecorder(q, log).Record(ctx, entry)
	// Output:
	// auditlog auditlog.record {"model_type":"widget","model_id":"w-1","method":"","path":"","created_by":"ann","payload":{"name":"gear"}}
	// level=ERROR msg="auditqueue: enqueue failed" model_type=widget model_id=w-1 err="connection refused"
}
