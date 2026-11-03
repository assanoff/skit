package dbx_test

import (
	"context"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/sqlite"
)

type widget struct {
	ID   int    `db:"id"`
	Name string `db:"name"`
}

const insertWidget = `INSERT INTO widgets (id, name) VALUES (:id, :name)`

// openShop opens an in-memory SQLite database with the tables of the examples.
func openShop(ctx context.Context) (*dbx.DB, error) {
	db, err := sqlite.Open(sqlite.Config{Path: ":memory:"})
	if err != nil {
		return nil, err
	}
	const schema = `
		CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE);
		CREATE TABLE stock (widget_id INTEGER PRIMARY KEY, qty INTEGER NOT NULL);`
	if err := dbx.Exec(ctx, db, schema); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

func countWidgets(ctx context.Context, q dbx.Querier) int {
	n, err := dbx.QueryOne[int](ctx, q, `SELECT count(*) FROM widgets`)
	if err != nil {
		return -1
	}
	return n
}

// A store runs its queries through the helpers: they scan the rows into a T
// and classify a failure into the package sentinels.
func Example() {
	ctx := context.Background()
	db, err := openShop(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = db.Close() }()

	for _, w := range []widget{{1, "gear"}, {2, "bolt"}, {3, "nut"}} {
		if err := dbx.NamedExec(ctx, db, insertWidget, w); err != nil {
			fmt.Println(err)
			return
		}
	}

	// One row by key: no rows is ErrDBNotFound.
	const get = `SELECT id, name FROM widgets WHERE id = :id`
	w, err := dbx.NamedQueryOne[widget](ctx, db, get, map[string]any{"id": 2})
	fmt.Println("get:", w, err)
	_, err = dbx.NamedQueryOne[widget](ctx, db, get, map[string]any{"id": 9})
	fmt.Println("not found:", errors.Is(err, dbx.ErrDBNotFound))

	// A list: no rows is an empty result, not an error.
	ws, err := dbx.QueryList[widget](ctx, db, `SELECT id, name FROM widgets ORDER BY name`)
	fmt.Println("list:", ws, err)

	// A unique violation is ErrDBDuplicatedEntry.
	err = dbx.NamedExec(ctx, db, insertWidget, widget{ID: 4, Name: "gear"})
	fmt.Println("duplicate:", errors.Is(err, dbx.ErrDBDuplicatedEntry))

	// The number of rows a statement changed.
	n, err := dbx.ExecRowsAffected(ctx, db, `DELETE FROM widgets WHERE id > 1`)
	fmt.Println("deleted:", n, err)
	// Output:
	// get: {2 bolt} <nil>
	// not found: true
	// list: [{2 bolt} {1 gear} {3 nut}] <nil>
	// duplicate: true
	// deleted: 2 <nil>
}

// WithinTran opens a transaction on a pool and joins one on a *Tx, so a
// function over a Querier runs in its caller's transaction when it has one.
func ExampleWithinTran() {
	ctx := context.Background()
	db, err := openShop(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = db.Close() }()

	add := func(q dbx.Querier, ws ...widget) error {
		return dbx.WithinTran(ctx, nil, q, func(tx *dbx.Tx) error {
			for _, w := range ws {
				if err := dbx.NamedExec(ctx, tx, insertWidget, w); err != nil {
					return err
				}
			}
			return nil
		})
	}

	// The second row breaks the unique name: the first does not commit.
	err = add(db, widget{1, "gear"}, widget{2, "gear"})
	fmt.Println("add:", errors.Is(err, dbx.ErrDBDuplicatedEntry), countWidgets(ctx, db))

	// Over a *Tx add joins: the transaction of the caller decides for both.
	err = dbx.WithinTran(ctx, nil, db, func(tx *dbx.Tx) error {
		if err := add(tx, widget{1, "gear"}); err != nil {
			return err
		}
		return errors.New("out of stock")
	})
	fmt.Println("checkout:", err, countWidgets(ctx, db))

	err = add(db, widget{1, "gear"}, widget{2, "bolt"})
	fmt.Println("add:", err, countWidgets(ctx, db))
	// Output:
	// add: true 0
	// checkout: out of stock 0
	// add: <nil> 2
}

// The stores of a domain hold a Querier, so one constructor serves the pool
// and a transaction.
type widgetStore struct{ q dbx.Querier }

func (s widgetStore) Add(ctx context.Context, w widget) error {
	return dbx.NamedExec(ctx, s.q, insertWidget, w)
}

type stockStore struct{ q dbx.Querier }

func (s stockStore) Put(ctx context.Context, id, qty int) error {
	const put = `INSERT INTO stock (widget_id, qty) VALUES (:id, :qty)`
	return dbx.NamedExec(ctx, s.q, put, map[string]any{"id": id, "qty": qty})
}

// A Tran runs a function on a set of stores bound to one transaction: bind
// builds the set from the transaction on every attempt.
func ExampleNewTran() {
	ctx := context.Background()
	db, err := openShop(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = db.Close() }()

	type unit struct {
		Widgets widgetStore
		Stock   stockStore
	}
	tran := dbx.NewTran(nil, db, func(tx *dbx.Tx) unit {
		return unit{Widgets: widgetStore{tx}, Stock: stockStore{tx}}
	})

	err = tran.WithinTran(ctx, func(u unit) error {
		if err := u.Widgets.Add(ctx, widget{1, "gear"}); err != nil {
			return err
		}
		return u.Stock.Put(ctx, 1, 40)
	})
	fmt.Println("receive:", err)

	const q = `SELECT w.name || ' ' || s.qty FROM widgets w JOIN stock s ON s.widget_id = w.id`
	stock, err := dbx.QueryList[string](ctx, db, q)
	fmt.Printf("stock: %q %v\n", stock, err)
	// Output:
	// receive: <nil>
	// stock: ["gear 40"] <nil>
}

// NamedQueryListUsingIn expands an IN (:ids) clause on any database; on
// Postgres ANY(:ids) binds the slice as one array instead.
func ExampleNamedQueryListUsingIn() {
	ctx := context.Background()
	db, err := openShop(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = db.Close() }()

	// BulkInsert writes the rows in batches; values are laid out row-major.
	rows := []any{1, "gear", 2, "bolt", 3, "nut"}
	if err := dbx.BulkInsert(ctx, db, "widgets", []string{"id", "name"}, rows, ""); err != nil {
		fmt.Println(err)
		return
	}

	const q = `SELECT id, name FROM widgets WHERE id IN (:ids) ORDER BY id`
	ws, err := dbx.NamedQueryListUsingIn[widget](ctx, db, q, map[string]any{"ids": []int{1, 3}})
	fmt.Println(ws, err)
	_, err = dbx.NamedQueryListUsingIn[widget](ctx, db, q, map[string]any{"ids": []int{}})
	fmt.Println(err)
	// Output:
	// [{1 gear} {3 nut}] <nil>
	// dbx: expand in: empty slice passed to 'in' query
}
