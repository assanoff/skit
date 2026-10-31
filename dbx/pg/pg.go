package pg

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/dbx"
)

// Postgres error codes the Classifier maps onto the dbx sentinels.
const (
	uniqueViolation      = "23505"
	deadlockDetected     = "40P01"
	serializationFailure = "40001"
)

// Pool limits for a zero field of Config. The connection lifetimes are those
// of pgxpool.
const (
	defaultMaxIdleConns    = 5
	defaultMaxOpenConns    = 20
	defaultConnMaxLifetime = time.Hour
	defaultConnMaxIdleTime = 30 * time.Minute
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
	// ConnMaxLifetime and ConnMaxIdleTime close a connection after that long
	// open or idle; zero keeps 1h and 30m.
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	DisableTLS      bool
	// Metrics is the registry of the pool metrics (go_sql_*, labeled db_name
	// with Name and host with Host); nil disables them.
	Metrics prometheus.Registerer
	// TracerProvider records a span of every query; nil → the global
	// provider.
	TracerProvider trace.TracerProvider
}

// Open opens a pool through the pgx driver, bound to Classifier. It does not
// verify connectivity; call dbx.StatusCheck for that. Every query is recorded
// as a span on cfg.TracerProvider. With cfg.Metrics set it registers the pool
// metrics; a pool of the same Host and Name already registered there is an
// error, and the new pool is closed.
func Open(cfg Config) (*dbx.DB, error) {
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
		return nil, fmt.Errorf("pg: open: %w", err)
	}
	pgCfg.Tracer = newQueryTracer(cfg.TracerProvider, pgCfg)
	db := stdlib.OpenDB(*pgCfg)

	db.SetMaxIdleConns(cmp.Or(cfg.MaxIdleConns, defaultMaxIdleConns))
	db.SetMaxOpenConns(cmp.Or(cfg.MaxOpenConns, defaultMaxOpenConns))
	db.SetConnMaxLifetime(cmp.Or(cfg.ConnMaxLifetime, defaultConnMaxLifetime))
	db.SetConnMaxIdleTime(cmp.Or(cfg.ConnMaxIdleTime, defaultConnMaxIdleTime))
	if err := registerPool(db, cfg); err != nil {
		return nil, errors.Join(err, db.Close())
	}

	return dbx.New(db, "pgx", Classifier{}), nil
}

// registerPool registers the pool metrics of db on cfg.Metrics, if set. They
// read db.Stats on each scrape, without a query.
func registerPool(db *sql.DB, cfg Config) error {
	if cfg.Metrics == nil {
		return nil
	}
	reg := prometheus.WrapRegistererWith(prometheus.Labels{"host": cfg.Host}, cfg.Metrics)
	if err := reg.Register(collectors.NewDBStatsCollector(db, cfg.Name)); err != nil {
		return fmt.Errorf("pg: register pool metrics of %s on %s: %w", cfg.Name, cfg.Host, err)
	}
	return nil
}

// sqlStater is a driver error with a SQLSTATE code: *pgconn.PgError, and the
// errors of lib/pq.
type sqlStater interface {
	error
	SQLState() string
}

// Classifier maps Postgres SQLSTATE codes onto the dbx sentinels: 23505 (unique
// violation) → dbx.ErrDBDuplicatedEntry, 40P01 (deadlock) and 40001
// (serialization failure) → dbx.ErrDBRetryable. It wraps the driver error, so
// errors.As still reaches it with its constraint and detail.
type Classifier struct{}

// Classify implements dbx.Classifier.
func (Classifier) Classify(err error) error {
	st, ok := errors.AsType[sqlStater](err)
	if !ok {
		return err
	}
	switch st.SQLState() {
	case uniqueViolation:
		return fmt.Errorf("%w: %w", dbx.ErrDBDuplicatedEntry, err)
	case deadlockDetected, serializationFailure:
		return fmt.Errorf("%w: %w", dbx.ErrDBRetryable, err)
	}
	return err
}
