package dim

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// resource represents a managed resource with automatic cleanup registration.
type resource[T any] struct {
	name      string
	factory   FactoryFunc[T]
	once      sync.Once
	value     T
	cleanupFn CleanupFunc
	err       error
}

// NewResource creates a new managed resource and returns Provider and CleanupFunc.
// factory must return: (resource, cleanup_function, error).
//
// Usage:
//
//	c.Store, cleanup := dim.NewResource("Store", func(ctx context.Context) (store.Store, dim.CleanupFunc, error) {
//	    st := postgres.New(c.Logger)
//	    if err := st.Connect(config); err != nil {
//	        return nil, nil, err
//	    }
//	    return st, func() error { return st.Close() }, nil
//	})
//	return cleanup, nil
//
// Note: Go cannot infer T from the named type FactoryFunc[T], so the signature
// takes an anonymous function literal.
func NewResource[T any](name string, factory func(context.Context) (T, CleanupFunc, error)) (Provider[T], CleanupFunc) {
	r := &resource[T]{
		name:    name,
		factory: factory,
	}
	return r.get, r.getCleanup()
}

// get returns the resource value, initializing it if necessary.
func (r *resource[T]) get(ctx context.Context) T {
	r.once.Do(func() {
		start := time.Now()
		slog.Info("initializing resource", slog.String("name", r.name))

		r.value, r.cleanupFn, r.err = r.factory(ctx)
		if r.err != nil {
			slog.Error("failed to initialize resource",
				slog.String("name", r.name),
				slog.String("error", r.err.Error()),
			)
			return
		}

		slog.Info("resource initialized",
			slog.String("name", r.name),
			slog.Duration("duration", time.Since(start)),
		)
	})
	// A failed init is fatal: re-panic on every call so a recovered first panic
	// cannot leave a caller holding a zero-value resource (sync.Once will never
	// re-run the factory).
	if r.err != nil {
		panic(r.err)
	}
	return r.value
}

// getCleanup returns the cleanup function for this resource.
func (r *resource[T]) getCleanup() CleanupFunc {
	// Wrap cleanup to add logging and a nil check.
	return NamedCleanup(r.name, func() error {
		if r.cleanupFn == nil {
			return nil
		}
		return r.cleanupFn()
	})
}
