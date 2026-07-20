// Package chx is a thin ClickHouse client for analytics / long-term stores.
//
// The write path uses the native clickhouse-go/v2 connection (driver.Conn),
// which speaks the columnar native protocol and supports PrepareBatch for large
// bulk inserts. Schema is applied via skit/migrate (goose, ClickHouse dialect)
// from a caller-supplied migrations fs.FS over a separate database/sql handle,
// because goose needs database/sql rather than the native conn.
//
// ClickHouse suits analytics / long-term storage: finished, structured data in
// large batches, never on a hot ingest path. This client is an optional building
// block — importing it is what pulls in the clickhouse-go/v2 dependency; wire it
// in only when a ClickHouse endpoint is configured.
package chx

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/pressly/goose/v3"

	"github.com/assanoff/skit/migrate"
)

// clickHouseDialect is goose's ClickHouse dialect. skit/migrate re-exports only
// Postgres/MySQL/SQLite3, but migrate.Dialect is an alias for goose.Dialect, so
// the constant is assignable directly.
const clickHouseDialect = migrate.Dialect(goose.DialectClickHouse)

// Config describes how to reach ClickHouse. Addrs is a list of host:port native
// endpoints (default native port 9000). Compression enables LZ4 wire compression,
// worthwhile for the large batches CH ingests.
type Config struct {
	Addrs        []string
	Database     string
	Username     string
	Password     string
	DialTimeout  time.Duration
	MaxOpenConns int
	Compression  bool
}

func (c Config) options() *clickhouse.Options {
	dial := c.DialTimeout
	if dial <= 0 {
		dial = 10 * time.Second
	}
	opts := &clickhouse.Options{
		Addr: c.Addrs,
		Auth: clickhouse.Auth{
			Database: c.Database,
			Username: c.Username,
			Password: c.Password,
		},
		DialTimeout:  dial,
		MaxOpenConns: c.MaxOpenConns,
	}
	if c.Compression {
		opts.Compression = &clickhouse.Compression{Method: clickhouse.CompressionLZ4}
	}
	return opts
}

// Client is a ClickHouse connection. It wraps the native driver.Conn; use Conn
// for direct access or the thin pass-throughs below.
type Client struct {
	conn driver.Conn
	cfg  Config
}

// Open dials ClickHouse and verifies connectivity with a ping. It does NOT apply
// migrations — call Migrate explicitly (usually once at startup) so schema changes
// stay an intentional operational step, not a side effect of connecting.
func Open(ctx context.Context, cfg Config) (*Client, error) {
	conn, err := clickhouse.Open(cfg.options())
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return &Client{conn: conn, cfg: cfg}, nil
}

// Migrate applies the pending DDL migrations in fsys (goose SQL files, rooted at
// the directory that holds them) using goose's ClickHouse dialect over a dedicated
// database/sql handle. It is idempotent. The caller supplies its own migrations
// FS, e.g. fs.Sub(embedded, "migrations"). goose needs a database/sql connection
// (clickhouse.OpenDB), separate from the native conn.
func Migrate(ctx context.Context, cfg Config, fsys fs.FS) error {
	// clickhouse.OpenDB never returns an error; connectivity surfaces on first use.
	db := clickhouse.OpenDB(cfg.options())
	// migrate.Close releases the goose provider but does NOT close db (the caller
	// owns the handle), so this Migrate owns and closes db itself.
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping clickhouse (migrations): %w", err)
	}
	m, err := migrate.New(clickHouseDialect, db, fsys)
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}
	defer m.Close()
	if err := m.Up(ctx); err != nil {
		return fmt.Errorf("apply clickhouse migrations: %w", err)
	}
	return nil
}

// Conn returns the underlying native connection for direct use (e.g. PrepareBatch
// with column-level control, or Select into structs).
func (c *Client) Conn() driver.Conn { return c.conn }

// Ping verifies the connection is alive.
func (c *Client) Ping(ctx context.Context) error { return c.conn.Ping(ctx) }

// Exec runs a statement with no result set (DDL/DML).
func (c *Client) Exec(ctx context.Context, query string, args ...any) error {
	return c.conn.Exec(ctx, query, args...)
}

// Select scans a full result set into dest (a pointer to a slice of structs).
func (c *Client) Select(ctx context.Context, dest any, query string, args ...any) error {
	return c.conn.Select(ctx, dest, query, args...)
}

// Query runs a query returning rows for manual iteration.
func (c *Client) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	return c.conn.Query(ctx, query, args...)
}

// PrepareBatch opens a client-side batch for a bulk INSERT — the primary write
// path (CH loves large batches).
func (c *Client) PrepareBatch(ctx context.Context, query string) (driver.Batch, error) {
	return c.conn.PrepareBatch(ctx, query)
}

// Name identifies this store for lifecycle logging.
func (c *Client) Name() string { return "ClickHouse" }

// Close releases the connection.
func (c *Client) Close() error { return c.conn.Close() }
