package migrate_test

import (
	"context"
	"errors"
	"fmt"
	"testing/fstest"

	_ "modernc.org/sqlite"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/sqlite"
	"github.com/assanoff/skit/migrate"
)

// migrations stands in for the embed.FS of a service's *.sql files.
var migrations = fstest.MapFS{
	"00001_widgets.sql": {Data: []byte(`-- +goose Up
CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL);

-- +goose Down
DROP TABLE widgets;
`)},
	"00002_widget_price.sql": {Data: []byte(`-- +goose Up
ALTER TABLE widgets ADD COLUMN price INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE widgets DROP COLUMN price;
`)},
}

// open opens an in-memory SQLite database and a Migrator over it.
func open() (*dbx.DB, *migrate.Migrator, error) {
	db, err := sqlite.Open(sqlite.Config{Path: ":memory:"})
	if err != nil {
		return nil, nil, err
	}
	// db.DB.DB is the *sql.DB of the pool.
	m, err := migrate.New(db.DB.DB, migrations, migrate.WithDialect(migrate.SQLite3))
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	return db, m, nil
}

// Up applies the pending migrations in version order; a second Up has nothing
// to do.
func Example() {
	ctx := context.Background()
	db, m, err := open()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = db.Close() }()

	for range 2 {
		if err := m.Up(ctx); err != nil {
			fmt.Println(err)
			return
		}
		v, err := m.Version(ctx)
		fmt.Println("version:", v, err)
	}
	// Output:
	// version: 2 <nil>
	// version: 2 <nil>
}

// Down rolls back the last applied migration; Status lists every migration
// with its state.
func ExampleMigrator_Down() {
	ctx := context.Background()
	db, m, err := open()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = db.Close() }()

	if err := m.Up(ctx); err != nil {
		fmt.Println(err)
		return
	}
	if err := m.Down(ctx); err != nil {
		fmt.Println(err)
		return
	}
	v, err := m.Version(ctx)
	fmt.Println("version:", v, err)

	st, err := m.Status(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, s := range st {
		fmt.Println(s.Version, s.Source, s.Applied)
	}
	// Output:
	// version: 1 <nil>
	// 1 00001_widgets.sql true
	// 2 00002_widget_price.sql false
}
