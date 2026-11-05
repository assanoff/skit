package partition_test

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/dbx/pg"
	"github.com/assanoff/skit/partition"
	"github.com/assanoff/skit/worker"
)

// A Loop runs Maintain on every replica: at the start and then every hour.
// The journal keeps 90 days, and the partitions of the next month are always
// there. The example needs Postgres, so go test builds it but does not run it.
func Example() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.Default()
	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		log.ErrorContext(ctx, "open", "err", err)
		return
	}
	defer func() { _ = db.Close() }()

	keeper, err := partition.New(log, db, partition.Config{
		Tables:    []string{"messages", "deliveries"},
		Period:    partition.Month,
		Retention: 90 * 24 * time.Hour,
	})
	if err != nil {
		log.ErrorContext(ctx, "partitions", "err", err)
		return
	}
	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	g.Add(worker.NewLoop(log, worker.LoopConfig{
		Name:               "partitions",
		Interval:           time.Hour,
		ImmediateFirstTick: true,
	}, keeper.Maintain))
	if err := g.Run(ctx); err != nil {
		log.ErrorContext(ctx, "group stopped", "err", err)
	}
}

// Day partitions with a week of rows, for a log of many rows a day; two days
// ahead leave a weekend to fix a failing run.
func ExampleNew_days() {
	log := slog.Default()
	db, err := pg.Open(pg.Config{Host: "localhost:5432", Name: "shop", DisableTLS: true})
	if err != nil {
		return
	}
	defer func() { _ = db.Close() }()

	keeper, err := partition.New(log, db, partition.Config{
		Tables:    []string{"audit.log"},
		Period:    partition.Day,
		Ahead:     2,
		Retention: 7 * 24 * time.Hour,
	})
	if err != nil {
		return
	}
	_ = keeper.Maintain(context.Background())
}
