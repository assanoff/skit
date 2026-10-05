package outbox_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
	"uuid"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/outbox"
)

// TestPGLeaseLifecycle drives the UUID-keyed rows (id, lease_id) through
// Postgres: insert, lease, the lease guard on MarkSent, and NULL lease_id after
// the row leaves in_flight.
func TestPGLeaseLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := context.Background()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	log := logger.New(io.Discard, logger.Config{Service: "test", Level: logger.LevelError})
	store := outbox.NewPG(log, pg.DB, outbox.Options{})
	is.NoErr(store.EnsureSchema(ctx))

	sent, err := outbox.NewEvent("widget.created", "widgets", "k", "application/json", []byte(`{}`), nil)
	is.NoErr(err)
	failed, err := outbox.NewEvent("widget.deleted", "widgets", "", "application/json", []byte(`{}`), nil)
	is.NoErr(err)
	is.NoErr(store.Insert(ctx, sent, failed))

	now := time.Now().UTC()
	leased, err := store.LeasePending(ctx, now, 10)
	is.NoErr(err)
	is.Equal(len(leased), 2) // both rows are claimed

	byID := map[uuid.UUID]outbox.Event{}
	for _, ev := range leased {
		is.True(ev.LeaseID != uuid.Nil()) // a leased row carries its lease id
		byID[ev.ID] = ev
	}
	gotSent, ok := byID[sent.ID]
	is.True(ok) // ids round-trip through the uuid column

	err = store.MarkSent(ctx, gotSent, uuid.New(), now)
	is.True(errors.Is(err, outbox.ErrLeaseLost)) // a foreign lease id must not match
	is.NoErr(store.MarkSent(ctx, gotSent, gotSent.LeaseID, now))
	is.NoErr(store.MarkFailed(ctx, byID[failed.ID], byID[failed.ID].LeaseID, "boom", now))

	stats, err := store.Stats(ctx, now)
	is.NoErr(err)
	is.Equal(stats.InFlight, int64(0)) // no row is left in flight
	is.Equal(stats.Pending, int64(1))  // the failed row is rescheduled

	again, err := store.LeasePending(ctx, now.Add(time.Hour), 10)
	is.NoErr(err)
	is.Equal(len(again), 1)                 // only the rescheduled row is pending
	is.Equal(again[0].ID, failed.ID)        // and it is the failed one
	is.Equal(again[0].LastError, "boom")    // with its error recorded
	is.True(again[0].LeaseID != uuid.Nil()) // re-leased under a fresh lease id
}
