package queue_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/queue"
)

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
