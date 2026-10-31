package dbx_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/matryer/is"
	_ "modernc.org/sqlite"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/sqlite"
)

var (
	discard = slog.New(slog.DiscardHandler)
	errBoom = errors.New("boom")
)

// openKV opens a SQLite database with an empty kv table.
func openKV(t *testing.T) *dbx.DB {
	t.Helper()
	db, err := sqlite.Open(sqlite.Config{Path: filepath.Join(t.TempDir(), "kv.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := dbx.Exec(t.Context(), db, `CREATE TABLE kv (k INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func insert(ctx context.Context, q dbx.Querier, k int) error {
	return dbx.NamedExec(ctx, q, `INSERT INTO kv (k) VALUES (:k)`, map[string]any{"k": k})
}

func count(t *testing.T, db *dbx.DB) int {
	t.Helper()
	n, err := dbx.QueryOne[int](t.Context(), db, `SELECT count(*) FROM kv`)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// WithinTran on a *Tx joins it: the joined writes commit with the outer
// transaction and roll back with it.
func TestWithinTranJoins(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	db := openKV(t)

	err := dbx.WithinTran(ctx, discard, db, func(tx *dbx.Tx) error {
		if err := insert(ctx, tx, 1); err != nil {
			return err
		}
		return dbx.WithinTran(ctx, discard, tx, func(joined *dbx.Tx) error {
			is.Equal(joined, tx) // the same transaction, not a new one
			return insert(ctx, joined, 2)
		})
	})
	is.NoErr(err)
	is.Equal(count(t, db), 2)

	err = dbx.WithinTran(ctx, discard, db, func(tx *dbx.Tx) error {
		joined := dbx.WithinTran(ctx, discard, tx, func(tx *dbx.Tx) error {
			return insert(ctx, tx, 3)
		})
		is.NoErr(joined) // the joined call does not commit on its own
		return errBoom
	})
	is.True(errors.Is(err, errBoom))
	is.Equal(count(t, db), 2) // the joined write rolled back with the outer one
}

// unit stands in for a domain Unit: whatever bind builds from tx.
type unit struct {
	tx *dbx.Tx
}

// A retryable error re-runs fn: bind runs again over a new transaction, and
// the write of the first attempt is gone.
func TestTranRerunBindsFresh(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	db := openKV(t)

	var bound []*dbx.Tx
	tr := dbx.NewTran(discard, db, func(tx *dbx.Tx) unit {
		bound = append(bound, tx)
		return unit{tx: tx}
	})
	attempt := 0
	err := tr.WithinTran(ctx, func(u unit) error {
		attempt++
		if err := insert(ctx, u.tx, attempt); err != nil {
			return err
		}
		if attempt == 1 {
			return dbx.ErrDBRetryable
		}
		return nil
	})
	is.NoErr(err)
	is.Equal(attempt, 2)
	is.Equal(len(bound), 2)
	is.True(bound[0] != bound[1]) // a fresh transaction on the re-run
	n, err := dbx.QueryOne[int](ctx, db, `SELECT k FROM kv`)
	is.NoErr(err)
	is.Equal(n, 2) // only the second attempt committed
}

// A Tran over a *Tx joins it.
func TestTranOverTxJoins(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	db := openKV(t)

	err := dbx.WithinTran(ctx, discard, db, func(tx *dbx.Tx) error {
		inner := dbx.NewTran(discard, tx, func(tx *dbx.Tx) unit {
			return unit{tx: tx}
		})
		is.NoErr(inner.WithinTran(ctx, func(u unit) error {
			return insert(ctx, u.tx, 1)
		}))
		return errBoom
	})
	is.True(errors.Is(err, errBoom))
	is.Equal(count(t, db), 0)
}
