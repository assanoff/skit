package pg_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/pg"
)

// Open builds the pool; StatusCheck waits until Postgres answers. Every query
// is a span, and the pool metrics are on the registry.
func Example() {
	ctx := context.Background()
	log := slog.Default()

	db, err := pg.Open(pg.Config{
		User: "postgres", Password: "postgres",
		Host: "localhost:5432", Name: "shop",
		DisableTLS: true,
		Metrics:    prometheus.NewRegistry(),
	})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()
	if err := dbx.StatusCheck(ctx, db); err != nil {
		log.ErrorContext(ctx, "status check", "err", err)
		return
	}

	// pgx binds a slice as one Postgres array: ANY(:ids) keeps the query text
	// the same for any number of ids.
	const q = `SELECT name FROM widgets WHERE id = ANY(:ids)`
	names, err := dbx.NamedQueryList[string](ctx, db, q, map[string]any{"ids": []int64{1, 3}})
	if err != nil {
		log.ErrorContext(ctx, "query", "err", err)
		return
	}
	log.InfoContext(ctx, "widgets", "names", names)
}

// Classify maps a SQLSTATE onto the dbx sentinels and keeps the driver error,
// so its constraint stays reachable.
func ExampleClassify() {
	driverErr := &pgconn.PgError{Code: "23505", ConstraintName: "widgets_name_key"}

	err := pg.Classify(fmt.Errorf("insert widget: %w", driverErr))
	fmt.Println(errors.Is(err, dbx.ErrDBDuplicatedEntry))
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		fmt.Println(pgErr.ConstraintName)
	}

	err = pg.Classify(&pgconn.PgError{Code: "40P01"})
	fmt.Println(errors.Is(err, dbx.ErrDBRetryable))
	// Output:
	// true
	// widgets_name_key
	// true
}
