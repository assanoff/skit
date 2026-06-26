// Package dbtest spins up disposable databases for integration tests using
// testcontainers.
//
// NewPostgres starts a Postgres container, opens a connection, optionally
// applies migrations, and registers teardown with t.Cleanup, so a test gets a
// clean, isolated database with one call. Importing this package pulls in
// testcontainers and the Docker client, so it is intended for test binaries
// only, and tests using it should be guarded with `if testing.Short() {
// t.Skip(...) }` since they require a running Docker daemon.
//
// # Usage
//
//	//go:embed migrations/*.sql
//	var migrationsFS embed.FS
//
//	func TestWidgets(t *testing.T) {
//	    if testing.Short() {
//	        t.Skip("requires docker")
//	    }
//	    ctx := context.Background()
//	    pg := dbtest.NewPostgres(ctx, t, dbtest.Config{Migrations: migrationsFS})
//
//	    repo := widget.NewStore(pg.DB) // pg.DB is an open, migrated *sqlx.DB
//	    // ... exercise repo against a real Postgres ...
//	}
//
// The returned Postgres also exposes Config, an dbx.Config pointing at the
// container, for handing to application code that opens its own pool.
//
// # Config
//
// The zero Config is valid; every field has a default:
//
//   - Image: container image (default "postgres:17-alpine").
//   - Database, User, Password: the bootstrapped database and superuser
//     (defaults "skit", "postgres", "postgres").
//   - Migrations: an fs.FS (typically an embed.FS of `*.sql` files) applied with
//     the migrate package once the database is ready; nil skips migrations.
//   - StartupTimeout: bound on waiting for the database to accept connections
//     (default 60s).
package dbtest
