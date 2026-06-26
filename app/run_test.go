package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/worker"
)

// blockingRunnable runs until its context is canceled.
type blockingRunnable struct{ name string }

func (b blockingRunnable) Name() string                    { return b.name }
func (b blockingRunnable) Start(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (b blockingRunnable) Stop(context.Context) error      { return nil }

func TestRunReturnsNilOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before Run starts: it supervises, sees Done, drains cleanly.

	err := Run(ctx, RunConfig{ShutdownTimeout: time.Second}, blockingRunnable{"a"}, blockingRunnable{"b"})
	if err != nil {
		t.Fatalf("Run = %v, want nil on ctx-driven shutdown", err)
	}
}

func TestRunCommandRunsSubsetThenFn(t *testing.T) {
	type deps struct{ steps []string }
	d := &deps{}

	inits := []Initializer[deps]{
		func(d *deps) (dim.CleanupFunc, error) { d.steps = append(d.steps, "store"); return nil, nil },
	}

	ctx := t.Context()

	var fnRan bool
	err := RunCommand(ctx, CommandConfig{}, d, inits, func(ctx context.Context, d *deps) error {
		fnRan = true
		d.steps = append(d.steps, "fn")
		return nil
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !fnRan {
		t.Fatal("fn did not run")
	}
	if len(d.steps) != 2 || d.steps[0] != "store" || d.steps[1] != "fn" {
		t.Fatalf("steps = %v, want [store fn]", d.steps)
	}
}

func TestRunCommandStopsWhenInitFails(t *testing.T) {
	type deps struct{}
	boom := errors.New("init boom")

	inits := []Initializer[deps]{
		func(*deps) (dim.CleanupFunc, error) { return nil, boom },
	}

	called := false
	err := RunCommand(context.Background(), CommandConfig{}, &deps{}, inits,
		func(context.Context, *deps) error { called = true; return nil })

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if called {
		t.Fatal("fn must not run when init fails")
	}
}

var _ worker.Runnable = blockingRunnable{}
