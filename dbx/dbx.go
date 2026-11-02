package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

// Sentinel errors returned by this package; match them with errors.Is. A
// Classifier wraps the driver error with ErrDBDuplicatedEntry or
// ErrDBRetryable, so errors.As still reaches it.
var (
	ErrDBNotFound        = sql.ErrNoRows
	ErrDBDuplicatedEntry = errors.New("duplicated entry")
	ErrDBRetryable       = errors.New("retryable")
)

// Classifier wraps a driver error with the dbx sentinel it recognizes —
// fmt.Errorf("%w: %w", ErrDBDuplicatedEntry, err) — or returns err unchanged.
// The presets ship one each, pg.Classify and sqlite.Classify; a pool of
// another driver brings its own function.
type Classifier func(err error) error

// Querier runs statements: a *DB on the pool, a *Tx in a transaction. Only
// this package implements it, so no other type decides whether WithinTran
// opens a transaction or joins one.
type Querier interface {
	sqlx.ExtContext
	within(ctx context.Context, log *slog.Logger, fn func(tx *Tx) error) error
	classify(err error) error
}

// DB is a pool bound to the Classifier of its engine.
type DB struct {
	*sqlx.DB

	cls Classifier
}

// New binds a database/sql pool. driverName selects the sqlx bind style
// ("pgx", "sqlite3", "mysql"), not the registered driver. With a nil c only
// ErrDBNotFound is recognized.
func New(db *sql.DB, driverName string, c Classifier) *DB {
	return &DB{DB: sqlx.NewDb(db, driverName), cls: c}
}

func (d *DB) classify(err error) error { return classify(d.cls, err) }

// Tx is a transaction opened by WithinTran. It has no Begin, Commit or
// Rollback: what is built over it can only join it, and the WithinTran that
// opened it ends it.
type Tx struct {
	tx  *sqlx.Tx
	cls Classifier
}

// DriverName returns the sqlx driver name.
func (t *Tx) DriverName() string { return t.tx.DriverName() }

// Rebind converts '?' placeholders to the bind style of the driver.
func (t *Tx) Rebind(query string) string { return t.tx.Rebind(query) }

// BindNamed expands the :name parameters of query from arg.
func (t *Tx) BindNamed(query string, arg any) (string, []any, error) {
	return t.tx.BindNamed(query, arg) //nolint:wrapcheck // the helpers classify it
}

// QueryContext runs a query that returns rows.
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...) //nolint:wrapcheck // the helpers classify it
}

// QueryxContext runs a query that returns sqlx rows.
func (t *Tx) QueryxContext(ctx context.Context, query string, args ...any) (*sqlx.Rows, error) {
	return t.tx.QueryxContext(ctx, query, args...) //nolint:wrapcheck // the helpers classify it
}

// QueryRowxContext runs a query that returns at most one row.
func (t *Tx) QueryRowxContext(ctx context.Context, query string, args ...any) *sqlx.Row {
	return t.tx.QueryRowxContext(ctx, query, args...)
}

// ExecContext runs a statement that returns no rows.
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...) //nolint:wrapcheck // the helpers classify it
}

func (t *Tx) classify(err error) error { return classify(t.cls, err) }

// classify maps no rows onto ErrDBNotFound and hands any other error to c.
func classify(c Classifier, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDBNotFound
	}
	if c == nil {
		return err
	}
	return c(err)
}

// DefaultStatusCheckTimeout bounds StatusCheck when it is called with a context
// that carries no deadline of its own (e.g. context.Background()). Without it a
// non-transient failure — wrong port, missing database, bad credentials, TLS
// mismatch — would retry forever and hang the caller silently. Pass a
// context.WithTimeout to choose a different bound.
const DefaultStatusCheckTimeout = 30 * time.Second

// StatusCheck pings the database, retrying until it is reachable or ctx is done.
//
// On timeout it returns errors.Join(ctx.Err(), lastPingErr) rather than a bare
// "context deadline exceeded", so the real cause — connection refused, TLS
// failure, password authentication failed — is visible at the call site instead
// of being masked by the deadline.
//
// If ctx has no deadline, DefaultStatusCheckTimeout is applied as a backstop so
// a persistent failure surfaces its error instead of hanging forever.
func StatusCheck(ctx context.Context, db *DB) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultStatusCheckTimeout)
		defer cancel()
	}

	var pingErr error
	for attempts := 1; ; attempts++ {
		pingErr = db.PingContext(ctx)
		if pingErr == nil {
			break
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), pingErr)
		case <-time.After(time.Duration(attempts) * 100 * time.Millisecond):
		}
	}

	var tmp bool
	const q = `SELECT true`
	if err := db.QueryRowContext(ctx, q).Scan(&tmp); err != nil {
		return fmt.Errorf("dbx: status check: %w", err)
	}
	return nil
}
