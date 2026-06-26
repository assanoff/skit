package app

import (
	"errors"
	"testing"

	"github.com/assanoff/skit/dim"
)

type testDeps struct {
	steps []string
}

func TestInitDepsRunsInOrderAndRegistersCleanups(t *testing.T) {
	d := &testDeps{}
	var cleanups []func() error
	register := func(fn func() error) { cleanups = append(cleanups, fn) }

	inits := []Initializer[testDeps]{
		func(d *testDeps) (dim.CleanupFunc, error) {
			d.steps = append(d.steps, "a")
			return func() error { d.steps = append(d.steps, "cleanup-a"); return nil }, nil
		},
		func(d *testDeps) (dim.CleanupFunc, error) {
			d.steps = append(d.steps, "b")
			return nil, nil // no cleanup -> must not be registered
		},
		func(d *testDeps) (dim.CleanupFunc, error) {
			d.steps = append(d.steps, "c")
			return func() error { d.steps = append(d.steps, "cleanup-c"); return nil }, nil
		},
	}

	if err := initDeps(d, inits, register); err != nil {
		t.Fatalf("initDeps: %v", err)
	}

	if got := d.steps; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("init order = %v, want [a b c]", got)
	}
	if len(cleanups) != 2 {
		t.Fatalf("registered %d cleanups, want 2 (the nil one is skipped)", len(cleanups))
	}
}

func TestInitDepsRunsSubsetForCommands(t *testing.T) {
	// A command assembles only the dependencies it needs by passing a subset of
	// the application's initializers — here "store + queue", omitting handlers.
	d := &testDeps{}
	var registered int
	register := func(func() error) { registered++ }

	initStore := func(d *testDeps) (dim.CleanupFunc, error) {
		d.steps = append(d.steps, "store")
		return func() error { return nil }, nil
	}
	initQueue := func(d *testDeps) (dim.CleanupFunc, error) {
		d.steps = append(d.steps, "queue")
		return func() error { return nil }, nil
	}
	initHandler := func(d *testDeps) (dim.CleanupFunc, error) {
		d.steps = append(d.steps, "handler")
		return nil, nil
	}

	all := []Initializer[testDeps]{initStore, initQueue, initHandler}
	subset := all[:2] // store + queue only

	if err := initDeps(d, subset, register); err != nil {
		t.Fatalf("initDeps: %v", err)
	}
	if len(d.steps) != 2 || d.steps[0] != "store" || d.steps[1] != "queue" {
		t.Fatalf("subset ran %v, want [store queue] (no handler)", d.steps)
	}
	if registered != 2 {
		t.Fatalf("registered = %d, want 2", registered)
	}
}

func TestInitDepsStopsAtFirstError(t *testing.T) {
	d := &testDeps{}
	var registered int
	register := func(func() error) { registered++ }
	boom := errors.New("boom")

	inits := []Initializer[testDeps]{
		func(d *testDeps) (dim.CleanupFunc, error) {
			d.steps = append(d.steps, "a")
			return func() error { return nil }, nil
		},
		func(d *testDeps) (dim.CleanupFunc, error) {
			d.steps = append(d.steps, "b")
			return nil, boom
		},
		func(d *testDeps) (dim.CleanupFunc, error) {
			d.steps = append(d.steps, "c") // must NOT run
			return nil, nil
		},
	}

	err := initDeps(d, inits, register)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if len(d.steps) != 2 || d.steps[1] != "b" {
		t.Fatalf("steps = %v, want to stop after [a b]", d.steps)
	}
	if registered != 1 {
		t.Fatalf("registered = %d, want 1 (only the first initializer's cleanup)", registered)
	}
}
