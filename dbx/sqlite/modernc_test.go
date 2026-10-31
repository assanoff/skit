package sqlite_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/matryer/is"
	_ "modernc.org/sqlite"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/sqlite"
)

// TestModernc runs the preset on the real modernc driver: the default driver
// name, the bind style of named queries, and the Classifier on both unique
// constraint codes.
func TestModernc(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	db, err := sqlite.Open(sqlite.Config{Path: filepath.Join(t.TempDir(), "app.db")})
	is.NoErr(err)
	t.Cleanup(func() { _ = db.Close() })
	is.NoErr(dbx.StatusCheck(ctx, db))

	is.NoErr(dbx.Exec(ctx, db, `CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT UNIQUE)`))
	const ins = `INSERT INTO tags (id, name) VALUES (:id, :name)`
	is.NoErr(dbx.NamedExec(ctx, db, ins, map[string]any{"id": 1, "name": "a"}))

	name, err := dbx.NamedQueryOne[string](ctx, db, `SELECT name FROM tags WHERE id = :id`,
		map[string]any{"id": 1})
	is.NoErr(err)
	is.Equal(name, "a")
	_, err = dbx.NamedQueryOne[string](ctx, db, `SELECT name FROM tags WHERE id = :id`,
		map[string]any{"id": 2})
	is.True(errors.Is(err, dbx.ErrDBNotFound))

	err = dbx.NamedExec(ctx, db, ins, map[string]any{"id": 1, "name": "b"})
	is.True(errors.Is(err, dbx.ErrDBDuplicatedEntry)) // primary key, 1555
	err = dbx.NamedExec(ctx, db, ins, map[string]any{"id": 2, "name": "a"})
	is.True(errors.Is(err, dbx.ErrDBDuplicatedEntry)) // unique, 2067
}

// An in-memory database keeps its tables across queries: the pool holds one
// connection.
func TestModerncInMemory(t *testing.T) {
	is := is.New(t)
	ctx := t.Context()
	db, err := sqlite.Open(sqlite.Config{Path: ":memory:"})
	is.NoErr(err)
	t.Cleanup(func() { _ = db.Close() })

	is.NoErr(dbx.Exec(ctx, db, `CREATE TABLE t (id INTEGER)`))
	n, err := dbx.QueryOne[int](ctx, db, `SELECT count(*) FROM t`)
	is.NoErr(err)
	is.Equal(n, 0)
}
