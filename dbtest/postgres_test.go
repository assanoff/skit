package dbtest_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbtest"
)

// TestStartPostgres covers the TestMain path: the caller starts the container,
// gets a migrated pool, and closes both itself.
func TestStartPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker")
	}

	is := is.New(t)
	ctx := context.Background()

	migrations := fstest.MapFS{
		"0001_widgets.sql": {Data: []byte(
			"-- +goose Up\nCREATE TABLE widgets (id int);\n" +
				"-- +goose Down\nDROP TABLE widgets;\n",
		)},
	}
	pg, err := dbtest.StartPostgres(ctx, dbtest.Config{Migrations: migrations})
	is.NoErr(err) // the container must start

	var n int
	err = pg.DB.GetContext(ctx, &n, "SELECT count(*) FROM widgets")
	is.NoErr(err) // the migration must have created the table
	is.Equal(n, 0)

	is.NoErr(pg.Close(ctx))                // closing must succeed
	is.True(pg.DB.PingContext(ctx) != nil) // the pool must be closed
}
