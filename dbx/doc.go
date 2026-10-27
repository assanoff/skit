// Package dbx provides SQL helpers on top of jmoiron/sqlx and database/sql:
// connection setup (Postgres through the pgx driver, SQLite through a driver
// the caller registers), named query/exec wrappers that log failed statements,
// transactions, and bulk insert/upsert. The helpers run on any driver; the
// error translation and the transaction retry know Postgres codes only.
//
// Open builds a *sqlx.DB from a Config; StatusCheck waits for it to become
// reachable. The Named* helpers run named queries/execs, log a failed
// statement with its expanded SQL at info level (the record's source is the
// store method that called the helper; ErrDBNotFound and ErrDBDuplicatedEntry,
// the outcomes the caller handles, are not logged), and translate well-known
// Postgres errors into the package sentinels (ErrDBNotFound,
// ErrDBDuplicatedEntry, ErrUndefinedTable) so callers match them with
// errors.Is instead of inspecting driver codes. The sentinel wraps the driver
// error: errors.As still reaches the *pgconn.PgError with its constraint and
// detail. WithinTran runs a function inside a transaction, committing on
// success and rolling back on error or panic, and transparently re-runs it
// when Postgres aborts it as a deadlock victim or on a serialization failure.
// Bulk* batch large multi-row writes within Postgres's parameter limit.
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
//	// Postgres only: pass a slice as a single array parameter; pgx binds it
//	// as a Postgres array, so ANY(:ids) filters without expanding
//	// placeholders. On other databases use NamedQuerySliceUsingIn.
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
// # Metrics
//
// With Config.Metrics set, Open registers the pool metrics there: go_sql_*
// (open, in-use and idle connections, waits, closes) labeled db_name with
// Config.Name and host with Config.Host, so a replica on another host is its
// own series. They read the pool stats on each scrape, without a query. A
// second pool of the same Host and Name on one registry is an error of Open.
//
// # Tracing
//
// Open records every query of the pool as a CLIENT span on
// Config.TracerProvider, named after the database (Config.Name). BEGIN and
// COMMIT are queries too, so a transaction shows as its own spans; StatusCheck
// pings past the tracer. The span carries db.system.name, db.namespace,
// server.address, server.port and db.query.text: the query as written, cut to
// 2 KiB. The arguments are never recorded — pass values as arguments, never in
// the query text. A Postgres error sets db.response.status_code and error.type
// to its SQLSTATE; no rows is not an error. A query from a suppressed context
// (otel.Suppress, the ticks of worker.Loop) records nothing.
//
// # Querying
//
//   - ExecContext / NamedExecContext run statements; the RowsAffected
//     variant returns the affected-row count.
//   - QueryStruct / NamedQueryStruct scan the first row (ErrDBNotFound when
//     there is none); an error later in the result is still returned.
//   - QuerySlice / NamedQuerySlice scan every row into a *[]T.
//   - NamedQuerySliceUsingIn / NamedQueryStructUsingIn handle a query with an IN
//     clause: they expand a slice parameter (sqlx.In) and rebind before running.
//
// For an IN-style filter on Postgres, bind the slice as a single array
// parameter and match with ANY(:ids) (see Usage): pgx encodes []int64/[]string
// as a Postgres array, so the query text stays constant regardless of list
// length. ANY works on Postgres only; the …UsingIn helpers expand IN (:ids) on
// any database.
//
// The plain (non-Named) variants take no parameters and run the query as
// written: it is not parsed for :name, so casts (::) and colons in literals
// pass through. The Named variants bind from a struct via its `db` tags.
//
// # UUID columns
//
// database/sql scans and binds the standard uuid.UUID, and pgx and
// modernc.org/sqlite take it as is. A driver that converts arguments on its
// own, such as go-sql-driver/mysql, rejects it: a store that must run there
// holds UUID instead (sql.Null[UUID] for a nullable column) and converts at
// the core/db boundary:
//
//	type dbWidget struct {
//	    ID dbx.UUID `db:"id"`
//	}
//	row := dbWidget{ID: dbx.UUID(w.ID)} // core -> db
//	w.ID = uuid.UUID(row.ID)            // db -> core
//
// Bind a lone UUID argument the same way: dbx.UUID(id). UUID binds through its
// string form, so on Postgres-only paths prefer uuid.UUID: a []UUID in ANY
// encodes about ten times slower.
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
// WithinTran runs a function in a transaction on a pool: it commits when the
// function returns nil and rolls back on an error or a panic.
//
// When two transactions lock the same rows in different orders Postgres aborts
// one of them with SQLSTATE 40P01 ("deadlock detected"); a serializable or
// repeatable read transaction that conflicts with another fails with 40001
// ("could not serialize access"). Either is rolled back in full. WithinTran
// treats both as transient: it re-runs the function in a fresh transaction
// with a short exponential backoff (3 attempts) and only then returns the
// error. The function must therefore be safe to run more than once — keep side
// effects inside the transaction and do not mutate captured state between
// runs. IsDeadlock exposes the deadlock check for statements that run outside
// WithinTran.
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
// MaxIdleConns and MaxOpenConns (zero keeps 5 idle and 20 open connections),
// DisableTLS (DisableTLS=true selects sslmode=disable, otherwise
// sslmode=require), Metrics (the registry of the pool metrics; nil disables
// them). Open always sets timezone=utc.
//
// OpenSQLite keeps 2 idle connections by default and leaves the open ones
// unbounded. An in-memory database keeps one connection that never closes:
// each connection to it opens its own empty database.
package dbx
