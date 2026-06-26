package dim

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Provider is a function that returns a resource value.
type Provider[T any] func(ctx context.Context) T

// CleanupFunc - function to close/clean up a resource.
// Functions are added in initialization order and are called in reverse order (LIFO).
type CleanupFunc func() error

// FactoryFunc is a function that creates a resource and returns it along with an optional cleanup function.
type FactoryFunc[T any] func(ctx context.Context) (T, CleanupFunc, error)

// NamedCleanup creates a function with closure on name that logs the resource name and execution time.
func NamedCleanup(name string, fn func() error) CleanupFunc {
	return func() error {
		start := time.Now()
		slog.Info("closing", slog.String("resource", name))

		if err := fn(); err != nil {
			slog.Error("failed to close",
				slog.String("resource", name),
				slog.String("error", err.Error()),
			)
			return err
		}

		slog.Info("closed",
			slog.String("resource", name),
			slog.Duration("duration", time.Since(start)),
		)
		return nil
	}
}

// Once wraps factory in a Provider that runs it a single time, caching the
// result. A factory error is fatal and panics — initialization failures should
// surface at startup, not be silently swallowed at call sites.
func Once[T any](factory func(ctx context.Context) (T, error)) Provider[T] {
	var (
		once sync.Once
		val  T
		err  error
	)
	return func(ctx context.Context) T {
		once.Do(func() {
			if val, err = factory(ctx); err != nil {
				panic(err)
			}
		})
		return val
	}
}

// OnceWithName creates a Provider with logging by dependency name.
func OnceWithName[T any](name string, factory func(ctx context.Context) (T, error)) Provider[T] {
	var (
		once sync.Once
		val  T
		err  error
	)
	return func(ctx context.Context) T {
		once.Do(func() {
			if val, err = factory(ctx); err != nil {
				panic(err)
			}
			slog.Info("successfully initialized", "resource", name)
		})
		return val
	}
}
