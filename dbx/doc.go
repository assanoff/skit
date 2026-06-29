// Package dbx provides Postgres helpers on top of jmoiron/sqlx and the pgx
// driver: connection setup, named query/exec wrappers with query logging,
// transactions, and bulk insert/upsert.
//
// Open builds a *sqlx.DB from a Config; StatusCheck waits for it to become
// reachable. The Named* helpers run named queries/execs, log the expanded SQL
// at debug level, and translate well-known Postgres errors into the package
// sentinels (ErrDBNotFound, ErrDBDuplicatedEntry, ErrUndefinedTable) so callers
// match them with errors.Is instead of inspecting driver codes. WithinTran runs
// a function inside a transaction, committing on success and rolling back on
// error or panic. Bulk* batch large multi-row writes within Postgres's
// parameter limit.
//
// # Usage
//
//	cfg := dbx.Config{
//	    User: "postgres", Password: "postgres",
//	    Host: "localhost:5432", Name: "app",
//	    Schema: "public", MaxIdleConns: 2, MaxOpenConns: 5,
//	    DisableTLS: true,
//	}
//	db, err := dbx.Open(cfg)
//	if err != nil {
//	    return err
//	}
//	if err := dbx.StatusCheck(ctx, db); err != nil {
//	    return err
//	}
//
//	// Named query into a slice.
//	var widgets []Widget
//	const q = `SELECT id, name FROM widgets WHERE name = :name`
//	arg := struct {
//	    Name string `db:"name"`
//	}{Name: "gadget"}
//	if err := dbx.NamedQuerySlice(ctx, log, db, q, arg, &widgets); err != nil {
//	    return err
//	}
//
//	// Pass a slice as a single array parameter: pgx binds it as a Postgres
//	// array, so ANY(:ids) filters without expanding placeholders.
//	var out []Widget
//	err := dbx.NamedQuerySlice(ctx, log, db,
//	    `SELECT * FROM widgets WHERE id = ANY(:ids)`,
//	    map[string]any{"ids": []int64{235, 401, 512}}, // slice -> one $1 -> int8[]
//	    &out,
//	)
//
//	// Write inside a transaction.
//	err = dbx.WithinTran(ctx, log, db, func(tx *sqlx.Tx) error {
//	    const ins = `INSERT INTO widgets (id, name) VALUES (:id, :name)`
//	    return dbx.NamedExecContext(ctx, log, tx, ins, w)
//	})
//	if errors.Is(err, dbx.ErrDBDuplicatedEntry) {
//	    // unique violation
//	}
//
// # Querying
//
//   - ExecContext / NamedExecContext run statements; the RowsAffected
//     variant returns the affected-row count.
//   - QueryStruct / NamedQueryStruct scan exactly one row (ErrDBNotFound when
//     none).
//   - QuerySlice / NamedQuerySlice scan every row into a *[]T.
//   - NamedQuerySliceUsingIn / NamedQueryStructUsingIn handle a query with an IN
//     clause: they expand a slice parameter (sqlx.In) and rebind before running.
//
// For an IN-style filter, prefer binding the slice as a single array parameter
// and matching with ANY(:ids) (see Usage): pgx encodes []int64/[]string as a
// Postgres array, so the query text stays constant regardless of list length.
// Reach for the …UsingIn helpers only when you need driver-agnostic placeholder
// expansion instead.
//
// The plain (non-Named) variants take no parameters; the Named variants bind
// from a struct via its `db` tags.
//
// # Dialects
//
// Subpackage dbx/dialect captures the few SQL fragments that differ between
// engines (currently pagination) behind a small Dialect interface, with Postgres
// and SQLite implementations, so a store can compose portable SQL.
//
// # Bulk writes
//
// BulkInsert writes len(values)/len(columns) rows, splitting into batches that
// stay under Postgres's bind-parameter cap, with values laid out row-major. An
// optional conflictAction (e.g. "ON CONFLICT DO NOTHING") is appended verbatim.
// BulkUpsert builds the ON CONFLICT … DO UPDATE clause for you from the
// conflict columns.
//
// # Transactions
//
// WithinTran is the common path. For stores that must run against either a pool
// or an outer transaction, depend on the Beginner / CommitRollbacker seam:
// NewBeginner adapts a *sqlx.DB, and ExtContext extracts the query surface from
// a started transaction.
//
// # Schema provisioning
//
// EnsureSchema applies idempotent DDL at startup under a transaction-scoped
// Postgres advisory lock, so several service replicas can provision the same
// table concurrently without racing — this is how the SDK storage packages
// (outbox, queue, auditlog, translation) create their own tables without a
// hand-written migration. AdvisoryKey derives a stable, collision-resistant lock
// key from a namespaced name (e.g. "skit.outbox.schema").
//
// # Config
//
// Config fields: User, Password, Host, Name, Schema (sets search_path),
// MaxIdleConns, MaxOpenConns, DisableTLS (DisableTLS=true selects
// sslmode=disable, otherwise sslmode=require). Open always sets timezone=utc.
package dbx
