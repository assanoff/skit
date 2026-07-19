package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/logger"
)

// Postgres error codes we translate into sentinel errors.
const (
	uniqueViolation = "23505"
	undefinedTable  = "42P01"
)

// Sentinel errors returned by this package; match them with errors.Is.
var (
	ErrDBNotFound        = sql.ErrNoRows
	ErrDBDuplicatedEntry = errors.New("duplicated entry")
	ErrUndefinedTable    = errors.New("undefined table")
)

// Config holds connection settings.
type Config struct {
	User         string
	Password     string
	Host         string
	Name         string
	Schema       string
	MaxIdleConns int
	MaxOpenConns int
	DisableTLS   bool
}

// Open opens a sqlx.DB using the pgx driver. It does not verify connectivity;
// call StatusCheck for that.
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

	db, err := sqlx.Open("pgx", u.String())
	if err != nil {
		return nil, fmt.Errorf("dbx: open: %w", err)
	}

	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetMaxOpenConns(cfg.MaxOpenConns)

	return db, nil
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
	DSN          string
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
	db, err := sqlx.Open(driver, cfg.dsn())
	if err != nil {
		return nil, fmt.Errorf("dbx: open sqlite: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	return db, nil
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
	return db.QueryRowContext(ctx, q).Scan(&tmp)
}

// ExecContext runs a parameterless statement.
func ExecContext(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string) error {
	return NamedExecContext(ctx, log, db, query, struct{}{})
}

// NamedExecContext runs an INSERT/UPDATE/DELETE with named parameters bound from
// data. It logs the expanded query and translates well-known Postgres errors.
func NamedExecContext(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, data any) error {
	_, err := namedExec(ctx, log, db, query, data)
	return err
}

// NamedExecContextRowsAffected is like NamedExecContext but returns the number
// of affected rows.
func NamedExecContextRowsAffected(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, data any) (int64, error) {
	res, err := namedExec(ctx, log, db, query, data)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func namedExec(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, data any) (sql.Result, error) {
	q := queryString(query, data)
	if log != nil {
		log.Debug(ctx, "dbx.exec", "query", q)
	}

	res, err := sqlx.NamedExecContext(ctx, db, query, data)
	if err != nil {
		return nil, translateError(err)
	}
	return res, nil
}

// QueryStruct runs a parameterless query expected to return exactly one row and
// scans it into dest (a pointer to a struct).
func QueryStruct(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, dest any) error {
	return NamedQueryStruct(ctx, log, db, query, struct{}{}, dest)
}

// NamedQueryStruct runs a named query expected to return exactly one row.
func NamedQueryStruct(ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, data, dest any) error {
	q := queryString(query, data)
	if log != nil {
		log.Debug(ctx, "dbx.query", "query", q)
	}

	rows, err := sqlx.NamedQueryContext(ctx, db, query, data)
	if err != nil {
		return translateError(err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return ErrDBNotFound
	}
	if err := rows.StructScan(dest); err != nil {
		return err
	}
	return nil
}

// QuerySlice runs a parameterless query and scans all rows into *[]T.
func QuerySlice[T any](ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, dest *[]T) error {
	return NamedQuerySlice(ctx, log, db, query, struct{}{}, dest)
}

// NamedQuerySlice runs a named query and scans all rows into *[]T.
func NamedQuerySlice[T any](ctx context.Context, log *logger.Logger, db sqlx.ExtContext, query string, data any, dest *[]T) error {
	q := queryString(query, data)
	if log != nil {
		log.Debug(ctx, "dbx.query", "query", q)
	}

	rows, err := sqlx.NamedQueryContext(ctx, db, query, data)
	if err != nil {
		return translateError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []T
	for rows.Next() {
		var v T
		if err := rows.StructScan(&v); err != nil {
			return err
		}
		out = append(out, v)
	}
	*dest = out
	return rows.Err()
}

func translateError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case uniqueViolation:
			return ErrDBDuplicatedEntry
		case undefinedTable:
			return ErrUndefinedTable
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDBNotFound
	}
	return err
}
