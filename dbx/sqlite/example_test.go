package sqlite_test

import (
	"context"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/sqlite"
)

// An in-memory database keeps one connection, so every query sees the same
// database. Named queries bind as ?, and a constraint violation is classified.
func Example() {
	ctx := context.Background()
	db, err := sqlite.Open(sqlite.Config{Path: ":memory:", ForeignKeys: true})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = db.Close() }()

	const schema = `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE)`
	if err := dbx.Exec(ctx, db, schema); err != nil {
		fmt.Println(err)
		return
	}
	const ins = `INSERT INTO users (id, email) VALUES (:id, :email)`
	err = dbx.NamedExec(ctx, db, ins, map[string]any{"id": 1, "email": "ann@example.com"})
	fmt.Println("insert:", err)

	err = dbx.NamedExec(ctx, db, ins, map[string]any{"id": 2, "email": "ann@example.com"})
	fmt.Println("duplicate:", errors.Is(err, dbx.ErrDBDuplicatedEntry))
	// The driver error stays reachable, with its extended result code.
	type coder interface {
		error
		Code() int
	}
	if c, ok := errors.AsType[coder](err); ok {
		fmt.Println("code:", c.Code())
	}
	// Output:
	// insert: <nil>
	// duplicate: true
	// code: 2067
}
