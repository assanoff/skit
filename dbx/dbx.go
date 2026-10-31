package dbx

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
// handle unchanged — BulkInsert rebinds placeholders to the SQLite bind style
// via db.Rebind.
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
