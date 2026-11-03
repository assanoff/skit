package dbtest_test

import (
	"context"
	"log"
	"testing"
	"testing/fstest"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/pg"
)

// migrations stands in for the embed.FS of a service's *.sql files.
var migrations = fstest.MapFS{
	"00001_widgets.sql": {Data: []byte(`-- +goose Up
CREATE TABLE widgets (id BIGINT PRIMARY KEY, name TEXT NOT NULL);

-- +goose Down
DROP TABLE widgets;
`)},
}

// A test gets a migrated database in a container of its own, removed when the
// test ends. It needs Docker: the test skips itself under -short.
func Example() {
	var t *testing.T // the *testing.T of the test
	is := is.New(t)
	ctx := context.Background()

	db := dbtest.NewPostgres(ctx, t, dbtest.Config{Migrations: migrations}).DB
	const ins = `INSERT INTO widgets (id, name) VALUES (:id, :name)`
	err := dbx.NamedExec(ctx, db, ins, map[string]any{"id": 1, "name": "gear"})
	is.NoErr(err)

	n, err := dbx.QueryOne[int](ctx, db, `SELECT count(*) FROM widgets`)
	is.NoErr(err)
	is.Equal(n, 1)
}

// StartPostgres runs one container for every test of a package, from
// TestMain; the caller closes it. Config points at the container, for code
// that opens its own pool.
func ExampleStartPostgres() {
	ctx := context.Background()
	container, err := dbtest.StartPostgres(ctx, dbtest.Config{Migrations: migrations})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = container.Close(ctx) }()

	db, err := pg.Open(container.Config)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := dbx.StatusCheck(ctx, db); err != nil {
		log.Fatal(err)
	}
}
