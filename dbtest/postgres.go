package dbtest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/migrate"
)

// Config tunes the Postgres container. The zero value is valid and uses the
// defaults documented on each field.
type Config struct {
	// Image is the container image (default "postgres:17-alpine").
	Image string
	// Database, User, Password name the bootstrapped database and superuser
	// (defaults "skit", "postgres", "postgres").
	Database string
	User     string
	Password string
	// Migrations, when non-nil, is applied with goose after the database is
	// ready — typically an embed.FS of `*.sql` files.
	Migrations fs.FS
	// StartupTimeout bounds waiting for the database to accept connections
	// (default 60s).
	StartupTimeout time.Duration
}

func (c Config) withDefaults() Config {
	c.Image = cmp.Or(c.Image, "postgres:17-alpine")
	c.Database = cmp.Or(c.Database, "skit")
	c.User = cmp.Or(c.User, "postgres")
	c.Password = cmp.Or(c.Password, "postgres")
	c.StartupTimeout = cmp.Or(c.StartupTimeout, 60*time.Second)
	return c
}

// Postgres is a running Postgres test container with an open connection.
type Postgres struct {
	// DB is an open, migrated connection pool to the container.
	DB *sqlx.DB
	// Config is an dbx.Config pointing at the container, suitable for handing
	// to application code that opens its own pool.
	Config dbx.Config

	container *tcpostgres.PostgresContainer
}

// NewPostgres starts a Postgres container with StartPostgres and registers
// Close with tb.Cleanup. It fails the test on any error. Skip the calling test
// under `go test -short`, as it needs Docker.
func NewPostgres(ctx context.Context, tb testing.TB, cfg Config) *Postgres {
	tb.Helper()

	p, err := StartPostgres(ctx, cfg)
	if err != nil {
		tb.Fatal(err)
	}
	// ctx may be tb.Context(), which is canceled before the cleanups run.
	tb.Cleanup(func() { _ = p.Close(context.WithoutCancel(ctx)) })
	return p
}

// StartPostgres starts a Postgres container, opens a connection and applies
// cfg.Migrations (when set). The caller calls Close, so it suits TestMain,
// where one container serves every test of the package. On error nothing is
// left running.
func StartPostgres(ctx context.Context, cfg Config) (*Postgres, error) {
	cfg = cfg.withDefaults()

	container, err := tcpostgres.Run(
		ctx, cfg.Image,
		tcpostgres.WithDatabase(cfg.Database),
		tcpostgres.WithUsername(cfg.User),
		tcpostgres.WithPassword(cfg.Password),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(cfg.StartupTimeout),
		),
	)
	if err != nil {
		// Run may return a created container along with the error.
		if container != nil {
			_ = container.Terminate(ctx)
		}
		return nil, fmt.Errorf("dbtest: start postgres: %w", err)
	}

	p := &Postgres{container: container}
	if err := p.connect(ctx, cfg); err != nil {
		return nil, errors.Join(err, p.Close(ctx))
	}
	return p, nil
}

// Close closes the pool and terminates the container.
func (p *Postgres) Close(ctx context.Context) error {
	var dbErr error
	if p.DB != nil {
		dbErr = p.DB.Close()
	}
	if err := errors.Join(dbErr, p.container.Terminate(ctx)); err != nil {
		return fmt.Errorf("dbtest: close postgres: %w", err)
	}
	return nil
}

// connect opens the pool to the running container and applies the migrations.
func (p *Postgres) connect(ctx context.Context, cfg Config) error {
	host, err := p.container.Host(ctx)
	if err != nil {
		return fmt.Errorf("dbtest: container host: %w", err)
	}
	port, err := p.container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return fmt.Errorf("dbtest: mapped port: %w", err)
	}

	p.Config = dbx.Config{
		User:         cfg.User,
		Password:     cfg.Password,
		Host:         host + ":" + port.Port(),
		Name:         cfg.Database,
		Schema:       "public",
		MaxIdleConns: 2,
		MaxOpenConns: 5,
		DisableTLS:   true,
	}
	db, err := dbx.Open(p.Config)
	if err != nil {
		return fmt.Errorf("dbtest: open db: %w", err)
	}
	p.DB = db

	if err := dbx.StatusCheck(ctx, db); err != nil {
		return fmt.Errorf("dbtest: status check: %w", err)
	}
	if cfg.Migrations == nil {
		return nil
	}
	return migrateUp(ctx, db, cfg.Migrations)
}

// migrateUp applies every migration in migrations to db.
func migrateUp(ctx context.Context, db *sqlx.DB, migrations fs.FS) error {
	m, err := migrate.New(db.DB, migrations)
	if err != nil {
		return fmt.Errorf("dbtest: build migrator: %w", err)
	}
	defer func() { _ = m.Close() }()

	if err := m.Up(ctx); err != nil {
		return fmt.Errorf("dbtest: apply migrations: %w", err)
	}
	return nil
}
