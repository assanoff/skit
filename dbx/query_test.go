package dbx_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
)

// TestQueryPostgres runs the query helpers against Postgres: an error after
// the first row, queries as written, a duplicate key and the pool defaults.
func TestQueryPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	ctx := context.Background()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	db := pg.DB

	t.Run("ErrorAfterFirstRow", func(t *testing.T) {
		is := is.New(t)
		var got struct {
			V int `db:"v"`
		}
		// The first row is 10, the second divides by zero.
		const q = `SELECT 10/(2-x) AS v FROM generate_series(1, 3) AS x`

		err := dbx.QueryStruct(ctx, nil, db, q, &got)
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		is.True(ok) // the error after the first row is not lost
		is.Equal(pgErr.Code, "22012")

		err = dbx.NamedQueryStruct(ctx, nil, db, q, struct{}{}, &got)
		_, ok = errors.AsType[*pgconn.PgError](err)
		is.True(ok)
	})

	t.Run("QueryAsWritten", func(t *testing.T) {
		is := is.New(t)
		const ddl = `CREATE TABLE notes (id int PRIMARY KEY, body text DEFAULT 'a:b'::text)`
		is.NoErr(dbx.ExecContext(ctx, nil, db, ddl)) // :: and :b pass through

		var one struct {
			V string `db:"v"`
		}
		is.NoErr(dbx.QueryStruct(ctx, nil, db, `SELECT 'x:y'::text AS v`, &one))
		is.Equal(one.V, "x:y")

		var all []struct {
			V int `db:"v"`
		}
		is.NoErr(dbx.QuerySlice(ctx, nil, db, `SELECT x::int AS v FROM generate_series(1, 2) AS x`, &all))
		is.Equal(len(all), 2)
	})

	t.Run("DuplicateKeepsDriverError", func(t *testing.T) {
		is := is.New(t)
		is.NoErr(dbx.ExecContext(ctx, nil, db, `CREATE TABLE dups (id int PRIMARY KEY)`))
		const ins = `INSERT INTO dups (id) VALUES (:id)`
		arg := struct {
			ID int `db:"id"`
		}{ID: 1}
		is.NoErr(dbx.NamedExecContext(ctx, nil, db, ins, arg))

		err := dbx.NamedExecContext(ctx, nil, db, ins, arg)
		is.True(errors.Is(err, dbx.ErrDBDuplicatedEntry))
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		is.True(ok) // constraint and detail survive the translation
		is.Equal(pgErr.ConstraintName, "dups_pkey")
	})

	t.Run("PoolDefaults", func(t *testing.T) {
		is := is.New(t)
		cfg := pg.Config
		cfg.MaxIdleConns, cfg.MaxOpenConns = 0, 0
		pool, err := dbx.Open(cfg)
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
	})
}
