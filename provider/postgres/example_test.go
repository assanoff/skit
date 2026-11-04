package postgres_test

import (
	"context"
	"log/slog"

	"github.com/assanoff/skit/closer"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/provider/postgres"
)

// The pool is a resource of the dependencies of a service: opened and checked
// on first use, closed by the cleanup at shutdown. The example needs Postgres,
// so go test builds it but does not run it.
func Example() {
	ctx := context.Background()
	log := slog.Default()
	db, cleanup := dim.NewResource("Store", postgres.Resource(pg.Config{
		User:     "shop",
		Password: "secret",
		Host:     "localhost:5432",
		Name:     "shop",
		Metrics:  metrics.New(),
	}))
	closer.Add(cleanup)

	if err := dbx.StatusCheck(ctx, db(ctx)); err != nil {
		log.ErrorContext(ctx, "status", "err", err)
	}
}
