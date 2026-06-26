package dbtest

import (
	"context"
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
	if c.Image == "" {
		c.Image = "postgres:17-alpine"
	}
	if c.Database == "" {
		c.Database = "skit"
	}
	if c.User == "" {
		c.User = "postgres"
	}
	if c.Password == "" {
		c.Password = "postgres"
	}
	if c.StartupTimeout == 0 {
		c.StartupTimeout = 60 * time.Second
	}
	return c
}

// Postgres is a running Postgres test container with an open connection.
type Postgres struct {
	// DB is an open, migrated connection pool to the container.
	DB *sqlx.DB
	// Config is an dbx.Config pointing at the container, suitable for handing
	// to application code that opens its own pool.
	Config dbx.Config
}

// NewPostgres starts a Postgres container, opens a connection, applies
// cfg.Migrations (when set), and registers cleanup. It fails the test on any
// error. Skip the calling test under `go test -short`, as it needs Docker.
func NewPostgres(ctx context.Context, t *testing.T, cfg Config) *Postgres {
	t.Helper()
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
		t.Fatalf("dbtest: start postgres: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("dbtest: container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("dbtest: mapped port: %v", err)
	}

	dbCfg := dbx.Config{
		User:         cfg.User,
		Password:     cfg.Password,
		Host:         host + ":" + port.Port(),
		Name:         cfg.Database,
		Schema:       "public",
		MaxIdleConns: 2,
		MaxOpenConns: 5,
		DisableTLS:   true,
	}

	db, err := dbx.Open(dbCfg)
	if err != nil {
		t.Fatalf("dbtest: open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := dbx.StatusCheck(ctx, db); err != nil {
		t.Fatalf("dbtest: status check: %v", err)
	}

	if cfg.Migrations != nil {
		m, err := migrate.New(migrate.Postgres, db.DB, cfg.Migrations)
		if err != nil {
			t.Fatalf("dbtest: build migrator: %v", err)
		}
		defer func() { _ = m.Close() }()
		if err := m.Up(ctx); err != nil {
			t.Fatalf("dbtest: apply migrations: %v", err)
		}
	}

	return &Postgres{DB: db, Config: dbCfg}
}
