// Package dbx provides SQL helpers on top of jmoiron/sqlx and database/sql:
// a pool and a transaction behind one Querier, query and exec helpers,
// transactions that open on a pool and join on a transaction, and bulk
// insert. It imports no driver: the presets dbx/pg (Postgres through pgx) and
// dbx/sqlite open a pool with a Classifier of their engine's errors, and
// New binds a pool of any other driver.
//
// The query helpers return their result and classify a failure into the
// package sentinels (ErrDBNotFound, ErrDBDuplicatedEntry, ErrDBRetryable), so
// callers match them with errors.Is instead of inspecting driver codes. The
// sentinel wraps the driver error: errors.As still reaches it with its details.
// They log nothing: the caller handles the error once, and a preset records the
// query on a span.
//
// # Usage
//
//	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "app"})
//	if err != nil {
//	    return err
//	}
//	if err := dbx.StatusCheck(ctx, db); err != nil {
//	    return err
//	}
//
//	// One row by key: no rows is ErrDBNotFound.
//	const get = `SELECT id, name FROM widgets WHERE id = :id`
//	w, err := dbx.NamedQueryOne[Widget](ctx, db, get, map[string]any{"id": id})
//	if errors.Is(err, dbx.ErrDBNotFound) {
//	    // the store returns its domain not-found error
//	}
//
//	// A list: no rows is an empty result, not an error.
//	const q = `SELECT id, name FROM widgets WHERE name = :name`
//	widgets, err := dbx.NamedQueryList[Widget](ctx, db, q, map[string]any{"name": "gadget"})
//
//	// A scalar.
//	n, err := dbx.QueryOne[int](ctx, db, `SELECT count(*) FROM widgets`)
//
//	// Postgres only: pass a slice as a single array parameter; pgx binds it
//	// as a Postgres array, so ANY(:ids) filters without expanding
//	// placeholders. On other databases use NamedQueryListUsingIn.
//	out, err := dbx.NamedQueryList[Widget](ctx, db,
//	    `SELECT * FROM widgets WHERE id = ANY(:ids)`,
//	    map[string]any{"ids": []int64{235, 401, 512}}, // slice -> one $1 -> int8[]
//	)
//
//	// Write inside a transaction.
//	err = dbx.WithinTran(ctx, log, db, func(tx *dbx.Tx) error {
//	    const ins = `INSERT INTO widgets (id, name) VALUES (:id, :name)`
//	    return dbx.NamedExec(ctx, tx, ins, w)
//	})
//	if errors.Is(err, dbx.ErrDBDuplicatedEntry) {
//	    // unique violation
//	}
//
// # Pool and transaction
//
// A *DB is a pool, a *Tx a transaction; both are a Querier, which every helper
// takes. Only this package implements Querier. A *Tx has no Begin, Commit or
// Rollback: what is built over it — a store, a Core over that store — can only
// join it. So a store holds a Querier and has one constructor for both:
//
//	widgetdb.NewStore(log, db) // WithinTran opens a transaction
//	widgetdb.NewStore(log, tx) // WithinTran joins tx
//
// Nothing goes through ctx: whether WithinTran opens or joins is decided by the
// type of the value passed in.
//
// # Errors
//
// A Classifier maps the errors of one engine onto the sentinels; the presets
// ship one each, and a pool of another driver passes its own to New. Without a
// Classifier only ErrDBNotFound is recognized. ErrDBRetryable marks an error
// after which the whole transaction may run again; on Postgres a deadlock
// victim (40P01) and a serialization failure (40001).
//
// # Querying
//
// Two families; the name tells the mode. Without a prefix a query takes no
// parameters and runs as written: it is not parsed for :name, so DDL, casts
// (::) and colons in literals pass through. The Named variants bind :name from
// a struct via its `db` tags or from a map[string]any; inside them write a cast
// as CAST(… AS …), since :: breaks the :name parsing.
//
//   - Exec / NamedExec run a statement; ExecRowsAffected /
//     NamedExecRowsAffected return the number of rows it changed.
//   - QueryOne / NamedQueryOne scan the first row into a struct or a scalar T;
//     no rows is ErrDBNotFound, and an error later in the result is returned.
//   - QueryList / NamedQueryList scan every row into a []T; no rows is a nil
//     slice and no error.
//   - NamedQueryListUsingIn handles a query with an IN (:ids) clause: it
//     expands the slice parameter (sqlx.In) and rebinds before running. An
//     empty slice is an error.
//
// What no rows means is decided where the meaning is known. A row by key is
// ErrDBNotFound, which the store turns into its domain not-found error. A list
// may be empty. The affected-row count means changed, not matched — MySQL
// without clientFoundRows counts an UPDATE to the same values as 0, a
// Postgres BEFORE trigger may skip a row — so the store that updates or
// deletes by key turns 0 into its not-found error, and other callers read the
// count as they need.
//
// For an IN-style filter on Postgres, bind the slice as a single array
// parameter and match with ANY(:ids) (see Usage): pgx encodes []int64/[]string
// as a Postgres array, so the query text stays constant regardless of list
// length. ANY works on Postgres only; NamedQueryListUsingIn expands IN (:ids)
// on any database.
//
// # Transactions
//
// WithinTran on a *DB opens a transaction: it commits when the function
// returns nil and rolls back on an error or a panic. An error the Classifier
// marks ErrDBRetryable rolls the transaction back in full, so WithinTran re-runs
// the function in a fresh transaction with a short exponential backoff (3
// attempts) and only then returns the error. The function must therefore be
// safe to run more than once — keep side effects inside the transaction and do
// not mutate captured state between runs.
//
// WithinTran on a *Tx joins it: the function runs in that transaction, and the
// WithinTran that opened it commits, rolls back or retries. A joined call is
// not a savepoint: if it wrote and failed, return its error, or half of its
// work commits.
//
// Tran is a transaction over a set the domain declares — repositories,
// publishers, the Cores of other domains — built by a function from the
// transaction on every attempt:
//
//	tran := dbx.NewTran(log, db, func(tx *dbx.Tx) widget.Unit {
//	    return widget.Unit{Repo: widgetdb.NewStore(log, tx), Users: users(tx)}
//	})
//	err := tran.WithinTran(ctx, func(u widget.Unit) error { ... })
//
// It satisfies the domain's interface with the same WithinTran method without
// an adapter. Over a *Tx it joins, so Trans nest.
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
// # Bulk writes
//
// BulkInsert writes len(values)/len(columns) rows, splitting into batches that
// stay under Postgres's bind-parameter cap, with values laid out row-major. An
// optional conflictAction (e.g. "ON CONFLICT DO NOTHING") is appended verbatim.
//
// # Schema provisioning
//
// EnsureSchema applies idempotent DDL at startup under a transaction-scoped
// Postgres advisory lock, so several service replicas can provision the same
// table concurrently without racing — this is how the SDK storage packages
// (outbox, queue, auditlog, translation) create their own tables without a
// hand-written migration. AdvisoryKey derives a stable, collision-resistant lock
// key from a namespaced name (e.g. "skit.outbox.schema").
package dbx
