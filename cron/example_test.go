package cron_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/cron"
	"github.com/assanoff/skit/worker"
)

// A Scheduler is a worker.Runnable: the Group starts it and stops it with the
// rest of the process. The job runs at 03:00 every day until the signal.
func Example() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.Default()

	sched := cron.New(log, cron.Config{Name: "reports"})
	err := sched.Add("0 3 * * *", "nightly-report", func(ctx context.Context) error {
		log.InfoContext(ctx, "building the nightly report")
		return nil
	})
	if err != nil {
		log.ErrorContext(ctx, "cron", "err", err)
		return
	}

	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	g.Add(sched)
	if err := g.Run(ctx); err != nil {
		log.ErrorContext(ctx, "group stopped", "err", err)
	}
}

// Add parses the spec at once, so a typo fails at wiring time instead of never
// firing.
func ExampleScheduler_Add() {
	sched := cron.New(nil, cron.Config{Name: "reports"})
	report := func(context.Context) error { return nil }

	for _, spec := range []string{"0 3 * * *", "@hourly", "@every 1h30m", "0 3 * *"} {
		if err := sched.Add(spec, "report", report); err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println("added", spec)
	}
	// Output:
	// added 0 3 * * *
	// added @hourly
	// added @every 1h30m
	// cron "reports": invalid spec "0 3 * *" for "report": expected exactly 5 fields, found 4: [0 3 * *]
}
