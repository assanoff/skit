package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
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
	log := slog.New(slog.DiscardHandler)
	store := outbox.NewPG(log, pg.DB, outbox.Options{})
	is.NoErr(store.EnsureSchema(ctx))

	sent, err := outbox.NewEvent("widget.created", "widgets", "application/json", []byte(`{}`), nil)
	is.NoErr(err)
	failed, err := outbox.NewEvent("widget.deleted", "widgets", "application/json", []byte(`{}`), nil)
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
	is.NoErr(store.MarkFailed(ctx, byID[failed.ID], byID[failed.ID].LeaseID, "boom", false, now))

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

// LeasePending returns its batch in next_attempt_at, created_at order, not in
// the order the rows were stored.
func TestPGLeaseOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	store := outbox.NewPG(slog.New(slog.DiscardHandler), pg.DB, outbox.Options{})
	is.NoErr(store.EnsureSchema(ctx))

	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	// Stored in this order, the reverse of the order they are due in.
	rows := []struct {
		typ     string
		created time.Duration // before base
		due     time.Duration // before base
	}{
		{"c", 1 * time.Millisecond, 0},
		{"b", 2 * time.Millisecond, 0},
		{"a", 3 * time.Millisecond, 0},
		{"d", 0, time.Second},
	}
	for _, r := range rows {
		ev, err := outbox.NewEvent(r.typ, "widgets", "application/json", []byte(`{}`), nil)
		is.NoErr(err)
		ev.CreatedAt = base.Add(-r.created)
		ev.NextAttemptAt = base.Add(-r.due)
		is.NoErr(store.Insert(ctx, ev))
	}

	leased, err := store.LeasePending(ctx, time.Now().UTC(), 10)
	is.NoErr(err)
	var types []string
	for _, ev := range leased {
		types = append(types, ev.Type)
	}
	is.Equal(types, []string{"d", "a", "b", "c"}) // due first, then created first
}

// TestPGFailAndRelease covers the failure path of the relay: a retryable
// failure reschedules the row with no attempt limit, a terminal one fails it,
// and Release returns the rest of the batch untouched.
func TestPGFailAndRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	store := outbox.NewPG(slog.New(slog.DiscardHandler), pg.DB, outbox.Options{})
	is.NoErr(store.EnsureSchema(ctx))

	var events []outbox.Event
	for _, typ := range []string{"a", "b", "c"} {
		ev, err := outbox.NewEvent(typ, "widgets", "application/json", []byte(`{}`), nil)
		is.NoErr(err)
		events = append(events, ev)
	}
	is.NoErr(store.Insert(ctx, events...))

	now := time.Now().UTC()
	leased, err := store.LeasePending(ctx, now, 10)
	is.NoErr(err)
	byType := map[string]outbox.Event{}
	for _, ev := range leased {
		byType[ev.Type] = ev
	}

	retried := byType["a"]
	retried.Attempts = retried.MaxAttempts + 5 // far past the old budget
	is.NoErr(store.MarkFailed(ctx, retried, retried.LeaseID, "down", false, now))
	is.NoErr(store.MarkFailed(ctx, byType["b"], byType["b"].LeaseID, "too large", true, now))
	n, err := store.Release(ctx, byType["c"].LeaseID)
	is.NoErr(err)
	is.Equal(n, int64(1)) // only the row still in flight

	stats, err := store.Stats(ctx, now)
	is.NoErr(err)
	is.Equal(stats.Pending, int64(2)) // retried and released
	is.Equal(stats.Failed, int64(1))  // only the terminal one
	is.Equal(stats.InFlight, int64(0))

	again, err := store.LeasePending(ctx, now, 10)
	is.NoErr(err)
	is.Equal(len(again), 1)        // the retried row waits for its backoff
	is.Equal(again[0].Type, "c")   // the released row is due at once
	is.Equal(again[0].Attempts, 0) // and its attempts stay as they were

	later, err := store.LeasePending(ctx, now.Add(time.Hour), 10)
	is.NoErr(err)
	is.Equal(len(later), 1)
	is.Equal(later[0].Type, "a")
	is.Equal(later[0].Attempts, retried.Attempts+1) // still counted, never limiting
}

type bound struct {
	ID string `json:"id"`
}

// TestBind checks that an event published through Bind lives and dies with
// the transaction it is bound to.
func TestBind(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	store := outbox.NewPG(log, pg.DB, outbox.Options{})
	is.NoErr(store.EnsureSchema(ctx))
	reg := outbox.NewRegistry()
	is.NoErr(reg.Register[bound]("widget.bound", "widgets"))

	boom := errors.New("boom")
	err := dbx.WithinTran(ctx, log, pg.DB, func(tx *dbx.Tx) error {
		is.NoErr(outbox.Bind(tx, store, reg).Publish(ctx, bound{ID: "1"}))
		return boom
	})
	is.True(errors.Is(err, boom))
	err = dbx.WithinTran(ctx, log, pg.DB, func(tx *dbx.Tx) error {
		return outbox.Bind(tx, store, reg).Publish(ctx, bound{ID: "2"})
	})
	is.NoErr(err)

	leased, err := store.LeasePending(ctx, time.Now().UTC(), 10)
	is.NoErr(err)
	is.Equal(len(leased), 1) // the rolled-back event is gone
	is.Equal(leased[0].Type, "widget.bound")
	is.Equal(string(leased[0].Payload), `{"id":"2"}`)
}
