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
// the first row, queries as written, no rows, named arguments, a duplicate key
// and the pool defaults.
func TestQueryPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	ctx := context.Background()
	pg := dbtest.NewPostgres(ctx, t, dbtest.Config{})
	db := pg.DB

	t.Run("ErrorAfterFirstRow", func(t *testing.T) {
		is := is.New(t)
		type row struct {
			V int `db:"v"`
		}
		// The first row is 10, the second divides by zero.
		const q = `SELECT 10/(2-x) AS v FROM generate_series(1, 3) AS x`

		_, err := dbx.QueryOne[row](ctx, db, q)
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		is.True(ok) // the error after the first row is not lost
		is.Equal(pgErr.Code, "22012")

		_, err = dbx.NamedQueryOne[row](ctx, db, q, map[string]any{})
		_, ok = errors.AsType[*pgconn.PgError](err)
		is.True(ok)
	})

	t.Run("QueryAsWritten", func(t *testing.T) {
		is := is.New(t)
		const ddl = `CREATE TABLE notes (id int PRIMARY KEY, body text DEFAULT 'a:b'::text)`
		is.NoErr(dbx.Exec(ctx, db, ddl)) // :: and :b pass through

		one, err := dbx.QueryOne[string](ctx, db, `SELECT 'x:y'::text`)
		is.NoErr(err)
		is.Equal(one, "x:y")

		all, err := dbx.QueryList[int](ctx, db, `SELECT x::int FROM generate_series(1, 2) AS x`)
		is.NoErr(err)
		is.Equal(all, []int{1, 2})
	})

	t.Run("NotFound", func(t *testing.T) {
		is := is.New(t)
		is.NoErr(dbx.Exec(ctx, db, `CREATE TABLE items (id int PRIMARY KEY, name text)`))
		arg := map[string]any{"id": 1}

		_, err := dbx.NamedQueryOne[int](ctx, db, `SELECT id FROM items WHERE id = :id`, arg)
		is.True(errors.Is(err, dbx.ErrDBNotFound)) // one row by key: an error

		rows, err := dbx.NamedQueryList[int](ctx, db, `SELECT id FROM items WHERE id = :id`, arg)
		is.NoErr(err) // a list: empty, not an error
		is.Equal(len(rows), 0)

		n, err := dbx.NamedExecRowsAffected(ctx, db, `DELETE FROM items WHERE id = :id`, arg)
		is.NoErr(err) // a count: the caller decides what 0 means
		is.Equal(n, int64(0))
	})

	t.Run("Named", func(t *testing.T) {
		is := is.New(t)
		is.NoErr(dbx.Exec(ctx, db, `CREATE TABLE tags (id int PRIMARY KEY, name text)`))
		type tag struct {
			ID   int    `db:"id"`
			Name string `db:"name"`
		}
		const ins = `INSERT INTO tags (id, name) VALUES (:id, :name)`
		is.NoErr(dbx.NamedExec(ctx, db, ins, tag{ID: 1, Name: "a"}))
		is.NoErr(dbx.NamedExec(ctx, db, ins, map[string]any{"id": 2, "name": "b"}))
		is.NoErr(dbx.BulkInsert(ctx, db, "tags", []string{"id", "name"}, []any{3, "c"}, ""))

		got, err := dbx.NamedQueryOne[tag](ctx, db, `SELECT id, name FROM tags WHERE id = :id`,
			map[string]any{"id": 2})
		is.NoErr(err)
		is.Equal(got, tag{ID: 2, Name: "b"})

		names, err := dbx.NamedQueryListUsingIn[string](ctx, db,
			`SELECT name FROM tags WHERE id IN (:ids) ORDER BY id`,
			map[string]any{"ids": []int{1, 3}})
		is.NoErr(err)
		is.Equal(names, []string{"a", "c"})

		n, err := dbx.NamedExecRowsAffected(ctx, db, `UPDATE tags SET name = :name WHERE id = :id`,
			tag{ID: 1, Name: "z"})
		is.NoErr(err)
		is.Equal(n, int64(1))

		count, err := dbx.QueryOne[int](ctx, db, `SELECT count(*) FROM tags`)
		is.NoErr(err)
		is.Equal(count, 3)
	})

	t.Run("DuplicateKeepsDriverError", func(t *testing.T) {
		is := is.New(t)
		is.NoErr(dbx.Exec(ctx, db, `CREATE TABLE dups (id int PRIMARY KEY)`))
		const ins = `INSERT INTO dups (id) VALUES (:id)`
		arg := struct {
			ID int `db:"id"`
		}{ID: 1}
		is.NoErr(dbx.NamedExec(ctx, db, ins, arg))

		err := dbx.NamedExec(ctx, db, ins, arg)
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
