package dim_test

import (
	"context"
	"errors"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dim"
)

func TestOnceCachesAndRunsFactoryOnce(t *testing.T) {
	is := is.New(t)

	calls := 0
	p := dim.Once(func(ctx context.Context) (int, error) {
		calls++
		return 42, nil
	})

	ctx := context.Background()
	is.Equal(p(ctx), 42)
	is.Equal(p(ctx), 42)
	is.Equal(calls, 1) // factory invoked exactly once despite repeated gets
}

func TestOncePanicsOnFactoryError(t *testing.T) {
	is := is.New(t)

	p := dim.Once(func(ctx context.Context) (int, error) {
		return 0, errors.New("init failed")
	})

	defer func() {
		is.True(recover() != nil) // a factory error is fatal -> panic at first get
	}()
	_ = p(context.Background())
	t.Fatal("expected panic")
}

func TestOnceWithNameCachesValue(t *testing.T) {
	is := is.New(t)

	calls := 0
	p := dim.OnceWithName("answer", func(ctx context.Context) (string, error) {
		calls++
		return "ok", nil
	})

	ctx := context.Background()
	is.Equal(p(ctx), "ok")
	is.Equal(p(ctx), "ok")
	is.Equal(calls, 1) // memoized like Once, with init logging
}

func TestNewResourceInitializesOnceAndCleans(t *testing.T) {
	is := is.New(t)

	inits := 0
	closes := 0
	provider, cleanup := dim.NewResource("store", func(ctx context.Context) (string, dim.CleanupFunc, error) {
		inits++
		return "conn", func() error {
			closes++
			return nil
		}, nil
	})

	ctx := context.Background()
	is.Equal(provider(ctx), "conn")
	is.Equal(provider(ctx), "conn")
	is.Equal(inits, 1) // factory runs once

	is.NoErr(cleanup())
	is.Equal(closes, 1) // cleanup invoked the resource's closer
}

func TestNewResourceCleanupBeforeInitIsNoop(t *testing.T) {
	is := is.New(t)

	_, cleanup := dim.NewResource("unused", func(ctx context.Context) (string, dim.CleanupFunc, error) {
		t.Fatal("factory should not run for an uninitialized resource")
		return "", nil, nil
	})

	is.NoErr(cleanup()) // never initialized -> cleanup is a safe no-op
}

func TestNewResourcePanicsOnFactoryError(t *testing.T) {
	is := is.New(t)

	provider, _ := dim.NewResource("bad", func(ctx context.Context) (string, dim.CleanupFunc, error) {
		return "", nil, errors.New("boom")
	})

	defer func() {
		is.True(recover() != nil) // init failure surfaces as a panic at startup
	}()
	_ = provider(context.Background())
	t.Fatal("expected panic")
}

func TestNamedCleanupPropagatesError(t *testing.T) {
	is := is.New(t)

	wantErr := errors.New("close failed")
	c := dim.NamedCleanup("thing", func() error {
		return wantErr
	})

	is.Equal(c(), wantErr) // the wrapped error is returned unchanged
}

func TestNamedCleanupSuccess(t *testing.T) {
	is := is.New(t)

	ran := false
	c := dim.NamedCleanup("thing", func() error {
		ran = true
		return nil
	})

	is.NoErr(c())
	is.True(ran) // the wrapped fn ran
}
