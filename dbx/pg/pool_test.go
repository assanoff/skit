package pg_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx/pg"
)

// TestPoolDefaults checks on a real Postgres that a zero Config bounds the
// pool and keeps idle connections.
func TestPoolDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}
	is := is.New(t)
	ctx := context.Background()
	cfg := dbtest.NewPostgres(ctx, t, dbtest.Config{}).Config
	cfg.MaxIdleConns, cfg.MaxOpenConns = 0, 0
	pool, err := pg.Open(cfg)
	is.NoErr(err)
	defer pool.Close()

	conns := make([]*sql.Conn, 6)
	for i := range conns {
		conns[i], err = pool.Conn(ctx)
		is.NoErr(err)
	}
	for _, c := range conns {
		is.NoErr(c.Close())
	}
	is.Equal(pool.Stats().MaxOpenConnections, 20)
	is.Equal(pool.Stats().Idle, 5) // zero MaxIdleConns kept none before
}
