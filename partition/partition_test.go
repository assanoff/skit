package partition_test

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/partition"
)

// journal is a message and its deliveries, partitioned by the month of the
// message: a delivery refers to its message.
const journal = `
CREATE TABLE messages (
	id         bigint NOT NULL,
	created_at timestamptz NOT NULL,
	PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
CREATE TABLE deliveries (
	id         bigint NOT NULL,
	message_id bigint NOT NULL,
	created_at timestamptz NOT NULL,
	PRIMARY KEY (id, created_at),
	FOREIGN KEY (message_id, created_at) REFERENCES messages (id, created_at)
) PARTITION BY RANGE (created_at)`

// startDB starts Postgres in a container and runs the ddl on it.
func startDB(t *testing.T, ddl string) *dbx.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test requires docker")
	}
	t.Parallel()
	db := dbtest.NewPostgres(t.Context(), t, dbtest.Config{}).DB
	if err := dbx.Exec(t.Context(), db, ddl); err != nil {
		t.Fatalf("create the tables: %v", err)
	}
	return db
}

// maintain runs the keeper of cfg at now.
func maintain(t *testing.T, db *dbx.DB, cfg partition.Config, now time.Time) {
	t.Helper()
	k, err := partition.New(slog.New(slog.DiscardHandler), db, cfg, at(now))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := k.Maintain(t.Context()); err != nil {
		t.Fatalf("Maintain() at %s error = %v", now, err)
	}
}

// at is the clock that always shows now.
func at(now time.Time) partition.Option {
	return partition.WithNow(func() time.Time { return now })
}

// partitions returns the names of the partitions of the journal.
func partitions(t *testing.T, db *dbx.DB) []string {
	t.Helper()
	const q = `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
WHERE i.inhparent IN ('messages'::regclass, 'deliveries'::regclass) ORDER BY 1`
	names, err := dbx.QueryList[string](t.Context(), db, q)
	if err != nil {
		t.Fatalf("read the partitions: %v", err)
	}
	return names
}

// exec runs a statement of the test.
func exec(t *testing.T, db *dbx.DB, query string) error {
	t.Helper()
	return dbx.Exec(t.Context(), db, query)
}

// count returns how many rows the query counts.
func count(t *testing.T, db *dbx.DB, query string) int {
	t.Helper()
	n, err := dbx.QueryOne[int](t.Context(), db, query)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// The partitions of October and November come before their rows; a row of
// December has no place. At the start of February with the retention of 90
// days October goes with its rows, deliveries first, November stays, and
// February and March get their partitions; a second run changes nothing.
func TestMaintainJournal(t *testing.T) {
	db := startDB(t, journal)
	is := is.New(t)
	cfg := partition.Config{
		Tables:    []string{"messages", "deliveries"},
		Period:    partition.Month,
		Retention: 90 * 24 * time.Hour,
	}

	maintain(t, db, cfg, time.Date(2026, 10, 15, 3, 30, 0, 0, time.UTC))
	is.NoErr(exec(t, db, `INSERT INTO messages VALUES
		(1, '2026-10-31 23:59:00+00'), (2, '2026-11-01 00:00:00+00')`))
	is.NoErr(exec(t, db, `INSERT INTO deliveries VALUES
		(1, 1, '2026-10-31 23:59:00+00'), (2, 2, '2026-11-01 00:00:00+00')`))
	err := exec(t, db, `INSERT INTO messages VALUES (3, '2026-12-01 00:00:00+00')`)
	is.True(err != nil) // December has no partition yet

	now := time.Date(2027, 2, 1, 3, 30, 0, 0, time.UTC)
	maintain(t, db, cfg, now)
	maintain(t, db, cfg, now)

	is.Equal(partitions(t, db), []string{
		"deliveries_2026_11",
		"deliveries_2027_02",
		"deliveries_2027_03",
		"messages_2026_11",
		"messages_2027_02",
		"messages_2027_03",
	})
	is.Equal(count(t, db, `SELECT count(*) FROM messages`), 1)   // November
	is.Equal(count(t, db, `SELECT count(*) FROM deliveries`), 1) // November
}

// Day partitions take columns of timestamp and date and a table in its own
// schema: the last second of a day and the start of the next land in their
// partitions.
func TestMaintainColumnTypes(t *testing.T) {
	db := startDB(t, `
CREATE TABLE stamps (at timestamp NOT NULL) PARTITION BY RANGE (at);
CREATE TABLE days (day date NOT NULL) PARTITION BY RANGE (day);
CREATE SCHEMA audit;
CREATE TABLE audit.log (at timestamptz NOT NULL) PARTITION BY RANGE (at)`)
	is := is.New(t)
	cfg := partition.Config{
		Tables: []string{"stamps", "days", "audit.log"},
		Period: partition.Day,
	}

	maintain(t, db, cfg, time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC))
	is.NoErr(exec(t, db, `INSERT INTO stamps VALUES
		('2026-11-05 23:59:59'), ('2026-11-06 00:00:00')`))
	is.NoErr(exec(t, db, `INSERT INTO days VALUES ('2026-11-05'), ('2026-11-06')`))
	is.NoErr(exec(t, db, `INSERT INTO audit.log VALUES
		('2026-11-05 23:59:59+00'), ('2026-11-06 00:00:00+00')`))

	for table, want := range map[string]int{
		"stamps_2026_11_05":    1,
		"stamps_2026_11_06":    1,
		"days_2026_11_05":      1,
		"days_2026_11_06":      1,
		"audit.log_2026_11_05": 1,
		"audit.log_2026_11_06": 1,
	} {
		is.Equal(count(t, db, "SELECT count(*) FROM "+table), want) // one row a day
	}
}

// Runs at once on several replicas take their turns: every one of them ends
// without an error, and each partition is made once.
func TestMaintainConcurrent(t *testing.T) {
	db := startDB(t, journal)
	is := is.New(t)
	cfg := partition.Config{
		Tables: []string{"messages", "deliveries"},
		Period: partition.Month,
	}
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		k, err := partition.New(slog.New(slog.DiscardHandler), db, cfg, at(now))
		is.NoErr(err)
		wg.Go(func() {
			errs[i] = k.Maintain(t.Context())
		})
	}
	wg.Wait()

	for _, err := range errs {
		is.NoErr(err) // no run fails on another
	}
	is.Equal(partitions(t, db), []string{
		"deliveries_2026_11",
		"deliveries_2026_12",
		"messages_2026_11",
		"messages_2026_12",
	})
}
