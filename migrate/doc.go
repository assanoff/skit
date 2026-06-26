// Package migrate applies SQL schema migrations from an embedded filesystem.
//
// It wraps github.com/pressly/goose using goose's Provider API, so callers
// never touch goose's process-global state (SetBaseFS/SetDialect) and several
// migrators can coexist in one process (e.g. app vs tests). Migrations are read
// from an fs.FS — typically an embed.FS of `*.sql` files — so they ship inside
// the binary.
//
// # Usage
//
//	//go:embed *.sql
//	var migrationsFS embed.FS
//
//	m, err := migrate.New(migrate.Postgres, db.DB, migrationsFS)
//	if err != nil {
//	    return err
//	}
//	defer m.Close()
//	if err := m.Up(ctx); err != nil {
//	    return err
//	}
//
// New takes a *sql.DB opened with a driver matching the dialect (e.g. a pgx
// connection for migrate.Postgres). It does not take ownership of db; Close
// releases only the provider, never the handle. Use fs.Sub if the migrations
// live in a subdirectory of the embedded FS.
//
// # Operations
//
//   - Up applies every pending migration in version order (a no-op when
//     current).
//   - Down rolls back the most recently applied migration.
//   - Version returns the current applied schema version (0 before any
//     migration).
//   - Status returns the applied state of every known migration, ordered by
//     version, as []MigrationStatus.
//
// # Dialects
//
// The supported dialects are re-exported so callers need not import goose:
// Postgres, MySQL, SQLite3. Dialect is an alias for goose.Dialect.
package migrate
