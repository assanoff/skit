package auditqueue_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/auditlog/auditqueue"
	"github.com/assanoff/skit/auditlog/audittest"
	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/worker"
)

// notifying is the store in memory that reports each saved entry on saved.
type notifying struct {
	*audittest.Store

	saved chan auditlog.AuditLog
}

func (s notifying) Save(ctx context.Context, al auditlog.AuditLog) error {
	if err := s.Store.Save(ctx, al); err != nil {
		return err
	}
	s.saved <- al
	return nil
}

// The workers write the entries Record scheduled through the core.
func TestWorkersRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	q := queue.NewPG(log, pg.DB)
	is.NoErr(q.EnsureSchema(ctx))

	rec := auditqueue.NewRecorder(q, log)
	for _, name := range []string{"gear", "big gear"} {
		rec.Record(ctx, auditlog.NewAuditLog{
			ModelType: "widget", ModelID: "w-1", CreatedBy: "ann",
			Payload: map[string]string{"name": name},
		})
	}

	store := notifying{Store: audittest.NewStore(), saved: make(chan auditlog.AuditLog, 2)}
	workers, err := auditqueue.NewWorkers(q, auditlog.NewCore(log, store), log,
		auditqueue.WorkerConfig{Interval: 10 * time.Millisecond})
	is.NoErr(err)
	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: time.Second})
	g.Add(workers...)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- g.Run(runCtx) }()

	var names []string
	for range 2 {
		al := <-store.saved
		is.Equal(al.CreatedBy, "ann")
		var w struct {
			Name string `json:"name"`
		}
		is.NoErr(json.Unmarshal(al.Payload, &w))
		names = append(names, w.Name)
	}
	cancel()
	<-done

	slices.Sort(names) // the workers may take the tasks in any order
	is.Equal(names, []string{"big gear", "gear"})
	vers, err := store.Versions(ctx, "widget", "w-1")
	is.NoErr(err)
	is.Equal(vers, []int{1, 2}) // two versions of the model
}
