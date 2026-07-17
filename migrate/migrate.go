package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Dialect identifies the database dialect for migrations.
type Dialect = goose.Dialect

// Supported dialects, re-exported so callers need not import goose directly.
const (
	Postgres = goose.DialectPostgres
	MySQL    = goose.DialectMySQL
	SQLite3  = goose.DialectSQLite3
)

// Migrator applies goose migrations against a database/sql handle. Construct it
// with New and release it with Close. It is safe to reuse across calls.
type Migrator struct {
	provider *goose.Provider
}

// New builds a Migrator for the given dialect, database handle and migration
// filesystem. fsys is usually an embed.FS rooted at the `*.sql` files; pass the
// embedded FS directly (use fs.Sub if the migrations live in a subdirectory).
//
// The handle must be opened with a driver matching dialect — e.g. a pgx/lib-pq
// connection for migrate.Postgres. New does not take ownership of db; Close
// releases only the provider.
func New(dialect Dialect, db *sql.DB, fsys fs.FS) (*Migrator, error) {
	p, err := goose.NewProvider(dialect, db, fsys)
	if err != nil {
		return nil, fmt.Errorf("new migration provider: %w", err)
	}
	return &Migrator{provider: p}, nil
}

// Up applies every pending migration in version order. It is a no-op when the
// schema is already current.
func (m *Migrator) Up(ctx context.Context) error {
	if _, err := m.provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// Down rolls back the most recently applied migration.
func (m *Migrator) Down(ctx context.Context) error {
	if _, err := m.provider.Down(ctx); err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

// Version returns the current applied schema version (0 before any migration).
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	v, err := m.provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrate version: %w", err)
	}
	return v, nil
}

// MigrationStatus is the applied state of a single migration.
type MigrationStatus struct {
	Version int64
	Source  string
	Applied bool
}

// Status returns the applied state of every known migration, ordered by version.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	st, err := m.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate status: %w", err)
	}
	out := make([]MigrationStatus, 0, len(st))
	for _, s := range st {
		out = append(out, MigrationStatus{
			Version: s.Source.Version,
			Source:  s.Source.Path,
			Applied: s.State == goose.StateApplied,
		})
	}
	return out, nil
}

// Close releases the Migrator. It deliberately does NOT close the underlying
// database handle passed to New: New does not take ownership, so the caller owns
// the handle's lifecycle. (goose's provider.Close would close the *sql.DB, which
// silently breaks callers that keep using their handle after migrating — e.g.
// dbtest.NewPostgres hands back a live pool. goose holds no other resources that
// require release once Up/Down/Status have returned.)
func (m *Migrator) Close() error {
	return nil
}
