// Package dbtest is a lightweight way to run Postgres in Docker for tests: one
// call starts a disposable container (via testcontainers), hands back an open,
// migrated pool, and removes the container when the test is done.
//
// NewPostgres starts a Postgres container, opens a connection, optionally
// applies migrations, and registers teardown with tb.Cleanup, so a test gets a
// clean, isolated database with one call. StartPostgres does the same without
// a testing.TB, for TestMain: one container serves every test of the package,
// and the caller closes it. Importing this package pulls in testcontainers and
// the Docker client, so it is intended for test binaries only, and tests using
// it should be guarded with `if testing.Short() { t.Skip(...) }` since they
// require a running Docker daemon.
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
// One container for the whole package, in TestMain. testing.Short needs the
// flags parsed, hence flag.Parse; under -short the tests run without it and
// skip themselves:
//
//	var pg *dbtest.Postgres
//
//	func TestMain(m *testing.M) {
//	    flag.Parse()
//	    if testing.Short() {
//	        os.Exit(m.Run())
//	    }
//	    ctx := context.Background()
//	    cfg := dbtest.Config{Migrations: migrationsFS}
//	    var err error
//	    pg, err = dbtest.StartPostgres(ctx, cfg)
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    code := m.Run()
//	    _ = pg.Close(ctx)
//	    os.Exit(code)
//	}
//
// The returned Postgres also exposes Config, an dbx.Config pointing at the
// container, for handing to application code that opens its own pool.
//
// # API
//
//   - NewPostgres: a container per test, closed by tb.Cleanup; fails the test
//     on error.
//   - StartPostgres / Close: a container the caller owns, for TestMain; on a
//     start error nothing is left running.
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
