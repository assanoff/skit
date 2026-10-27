package dbx

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel/trace"
)

// Postgres error codes we translate into sentinel errors or retry.
const (
	uniqueViolation      = "23505"
	undefinedTable       = "42P01"
	deadlockDetected     = "40P01"
	serializationFailure = "40001"
)

// Pool sizes for a zero field of Config and SQLiteConfig.
const (
	defaultMaxIdleConns       = 5
	defaultMaxOpenConns       = 20
	defaultSQLiteMaxIdleConns = 2
)

// Sentinel errors returned by this package; match them with errors.Is.
var (
	ErrDBNotFound        = sql.ErrNoRows
	ErrDBDuplicatedEntry = errors.New("duplicated entry")
	ErrUndefinedTable    = errors.New("undefined table")
)

// Config holds connection settings.
type Config struct {
	User     string
	Password string
	Host     string
	Name     string
	Schema   string
	// MaxIdleConns and MaxOpenConns bound the pool; zero keeps 5 idle and 20
	// open connections.
	MaxIdleConns int
	MaxOpenConns int
	DisableTLS   bool
	// Metrics is the registry of the pool metrics (go_sql_*, labeled db_name
	// with Name and host with Host); nil disables them.
	Metrics prometheus.Registerer
	// TracerProvider records a span of every query; nil → the global
	// provider.
	TracerProvider trace.TracerProvider
}

// Open opens a sqlx.DB using the pgx driver. It does not verify connectivity;
// call StatusCheck for that. Every query is recorded as a span on
// cfg.TracerProvider. With cfg.Metrics set it registers the pool metrics; a
// pool of the same Host and Name already registered there is an error, and the
// new pool is closed.
func Open(cfg Config) (*sqlx.DB, error) {
	sslMode := "require"
	if cfg.DisableTLS {
		sslMode = "disable"
	}

	q := make(url.Values)
	q.Set("sslmode", sslMode)
	q.Set("timezone", "utc")
	if cfg.Schema != "" {
		q.Set("search_path", cfg.Schema)
	}

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     cfg.Host,
		Path:     cfg.Name,
		RawQuery: q.Encode(),
	}

	pgCfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("dbx: open: %w", err)
	}
	pgCfg.Tracer = newQueryTracer(cfg.TracerProvider, pgCfg)
	db := sqlx.NewDb(stdlib.OpenDB(*pgCfg), "pgx")

	db.SetMaxIdleConns(cmp.Or(cfg.MaxIdleConns, defaultMaxIdleConns))
	db.SetMaxOpenConns(cmp.Or(cfg.MaxOpenConns, defaultMaxOpenConns))
	if err := registerPool(db, cfg); err != nil {
		return nil, errors.Join(err, db.Close())
	}

	return db, nil
}

// registerPool registers the pool metrics of db on cfg.Metrics, if set. They
// read db.Stats on each scrape, without a query.
func registerPool(db *sqlx.DB, cfg Config) error {
	if cfg.Metrics == nil {
		return nil
	}
	reg := prometheus.WrapRegistererWith(prometheus.Labels{"host": cfg.Host}, cfg.Metrics)
	if err := reg.Register(collectors.NewDBStatsCollector(db.DB, cfg.Name)); err != nil {
		return fmt.Errorf("dbx: register pool metrics of %s on %s: %w", cfg.Name, cfg.Host, err)
	}
	return nil
}

// SQLiteConfig configures a SQLite database opened by OpenSQLite.
type SQLiteConfig struct {
	// Path is the database file (or ":memory:").
	Path string
	// Driver is the registered database/sql driver name; empty defaults to
	// "sqlite" (modernc.org/sqlite, pure Go). The caller blank-imports the driver
	// — dbx adds no SQLite driver dependency of its own.
	Driver string
	// Pragmas applied via the modernc DSN (_pragma=…); ignored when DSN is set.
	JournalMode string        // e.g. "WAL"; empty keeps the engine default
	BusyTimeout time.Duration // busy_timeout; zero keeps the engine default
	ForeignKeys bool          // foreign_keys(1)
	// DSN, when set, is used verbatim as the driver DSN — for drivers whose DSN
	// syntax differs from modernc (e.g. mattn/go-sqlite3).
	DSN string
	// MaxOpenConns and MaxIdleConns bound the pool; zero leaves the open
	// connections unbounded and keeps 2 idle. An in-memory database
	// (":memory:" or mode=memory) keeps one connection by default: each
	// connection to it opens its own empty database.
	MaxOpenConns int
	MaxIdleConns int
}

// DefaultSQLiteDriver is the driver name OpenSQLite uses when Config.Driver is
// empty — modernc.org/sqlite, the pure-Go driver (no cgo).
const DefaultSQLiteDriver = "sqlite"

// OpenSQLite opens a sqlx.DB for SQLite using a caller-registered driver (default
// "sqlite" = modernc.org/sqlite). It does not verify connectivity; call
// StatusCheck for that. All the dbx query helpers work against the returned
// handle unchanged — BulkInsert/BulkUpsert rebind placeholders to the SQLite bind
// style via db.Rebind.
func OpenSQLite(cfg SQLiteConfig) (*sqlx.DB, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = DefaultSQLiteDriver
	}
	dsn := cfg.dsn()
	db, err := sqlx.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("dbx: open sqlite: %w", err)
	}
	if inMemory(dsn) {
		// One connection that never closes: the database lives as long as it
		// does, and every query sees the same one.
		db.SetMaxIdleConns(cmp.Or(cfg.MaxIdleConns, 1))
		db.SetMaxOpenConns(cmp.Or(cfg.MaxOpenConns, 1))
		return db, nil
	}
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	db.SetMaxIdleConns(cmp.Or(cfg.MaxIdleConns, defaultSQLiteMaxIdleConns))
	return db, nil
}

// inMemory reports whether dsn opens an in-memory SQLite database.
func inMemory(dsn string) bool {
	return strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory")
}

// dsn returns cfg.DSN verbatim when set, else builds a modernc.org/sqlite DSN
// (file:<path>?_pragma=…) from Path and the pragma fields.
func (cfg SQLiteConfig) dsn() string {
	if cfg.DSN != "" {
		return cfg.DSN
	}
	var pragmas []string
	if cfg.BusyTimeout > 0 {
		pragmas = append(pragmas, fmt.Sprintf("_pragma=busy_timeout(%d)", cfg.BusyTimeout.Milliseconds()))
	}
	if cfg.JournalMode != "" {
		pragmas = append(pragmas, "_pragma=journal_mode("+cfg.JournalMode+")")
	}
	if cfg.ForeignKeys {
		pragmas = append(pragmas, "_pragma=foreign_keys(1)")
	}
	dsn := "file:" + cfg.Path
	if len(pragmas) > 0 {
		dsn += "?" + strings.Join(pragmas, "&")
	}
	return dsn
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
func StatusCheck(ctx context.Context, db *sqlx.DB) error {
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

// ExecContext runs a parameterless statement as written: the query is not
// parsed for :name, so casts (::) and colons in literals pass through.
func ExecContext(ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string) error {
	_, err := db.ExecContext(ctx, query)
	return logQuery(ctx, log, caller(), "dbx.exec", query, nil, translateError(err))
}

// NamedExecContext runs an INSERT/UPDATE/DELETE with named parameters bound from
// data. It translates well-known Postgres errors and logs a failed statement
// with the expanded query.
func NamedExecContext(ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string, data any) error {
	_, err := namedExec(ctx, db, query, data)
	return logQuery(ctx, log, caller(), "dbx.exec", query, data, err)
}

// NamedExecContextRowsAffected is like NamedExecContext but returns the number
// of affected rows.
func NamedExecContextRowsAffected(ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string, data any) (int64, error) {
	res, err := namedExec(ctx, db, query, data)
	if err != nil {
		return 0, logQuery(ctx, log, caller(), "dbx.exec", query, data, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("dbx: rows affected: %w", err)
	}
	return n, nil
}

func namedExec(ctx context.Context, db sqlx.ExtContext, query string, data any) (sql.Result, error) {
	res, err := sqlx.NamedExecContext(ctx, db, query, data)
	if err != nil {
		return nil, translateError(err)
	}
	return res, nil
}

// QueryStruct runs a parameterless query as written (no :name parsing) and
// scans its first row into dest (a pointer to a struct).
func QueryStruct(ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string, dest any) error {
	err := queryStruct(ctx, db, query, dest)
	return logQuery(ctx, log, caller(), "dbx.query", query, nil, err)
}

// NamedQueryStruct runs a named query and scans its first row into dest.
func NamedQueryStruct(ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string, data, dest any) error {
	err := namedQueryStruct(ctx, db, query, data, dest)
	return logQuery(ctx, log, caller(), "dbx.query", query, data, err)
}

func queryStruct(ctx context.Context, db sqlx.ExtContext, query string, dest any) error {
	rows, err := db.QueryxContext(ctx, query)
	if err != nil {
		return translateError(err)
	}
	return scanFirst(rows, dest)
}

func namedQueryStruct(ctx context.Context, db sqlx.ExtContext, query string, data, dest any) error {
	rows, err := sqlx.NamedQueryContext(ctx, db, query, data)
	if err != nil {
		return translateError(err)
	}
	return scanFirst(rows, dest)
}

// scanFirst scans the first row of rows into dest and closes rows. Close reads
// the rest of the result, so an error that comes after the first row — a
// division by zero in the third — is returned, not lost.
func scanFirst(rows *sqlx.Rows, dest any) error {
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		// rows.Next returning false also covers a mid-stream failure (conn
		// reset, ctx cancel); surface it instead of reporting "not found".
		if err := rows.Err(); err != nil {
			return translateError(err)
		}
		return ErrDBNotFound
	}
	if err := rows.StructScan(dest); err != nil {
		return fmt.Errorf("dbx: scan: %w", err)
	}
	return translateError(rows.Close())
}

// QuerySlice runs a parameterless query as written (no :name parsing) and
// scans all rows into *[]T.
func QuerySlice[T any](ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string, dest *[]T) error {
	err := querySlice(ctx, db, query, dest)
	return logQuery(ctx, log, caller(), "dbx.query", query, nil, err)
}

// NamedQuerySlice runs a named query and scans all rows into *[]T.
func NamedQuerySlice[T any](ctx context.Context, log *slog.Logger, db sqlx.ExtContext, query string, data any, dest *[]T) error {
	err := namedQuerySlice(ctx, db, query, data, dest)
	return logQuery(ctx, log, caller(), "dbx.query", query, data, err)
}

func querySlice[T any](ctx context.Context, db sqlx.ExtContext, query string, dest *[]T) error {
	rows, err := db.QueryxContext(ctx, query)
	if err != nil {
		return translateError(err)
	}
	return scanAll(rows, dest)
}

func namedQuerySlice[T any](ctx context.Context, db sqlx.ExtContext, query string, data any, dest *[]T) error {
	rows, err := sqlx.NamedQueryContext(ctx, db, query, data)
	if err != nil {
		return translateError(err)
	}
	return scanAll(rows, dest)
}

// scanAll scans every row of rows into *dest and closes rows.
func scanAll[T any](rows *sqlx.Rows, dest *[]T) error {
	defer func() { _ = rows.Close() }()

	var out []T
	for rows.Next() {
		var v T
		if err := rows.StructScan(&v); err != nil {
			return fmt.Errorf("dbx: scan: %w", err)
		}
		out = append(out, v)
	}
	*dest = out
	return translateError(rows.Err())
}

// translateError maps well-known Postgres errors onto the package sentinels.
// It wraps the driver error rather than replacing it, so the *pgconn.PgError
// with its constraint and detail stays reachable through errors.As.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case uniqueViolation:
			return fmt.Errorf("%w: %w", ErrDBDuplicatedEntry, err)
		case undefinedTable:
			return fmt.Errorf("%w: %w", ErrUndefinedTable, err)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDBNotFound
	}
	return err
}
