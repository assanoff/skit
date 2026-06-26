// Package dim is a slim, generics-based dependency-injection toolkit: lazy
// providers and managed resources built from plain functions, with structured
// logging of initialization and cleanup.
//
// A Provider[T] is just a function that returns a value, evaluated lazily and
// (via Once / OnceWithName / NewResource) memoized so the resource is built
// exactly once on first use. NewResource additionally pairs the provider with
// a CleanupFunc, giving each resource an automatically registered teardown that
// the application can run in reverse (LIFO) order at shutdown, so applications
// can adopt a single, consistent wiring style.
//
// # Usage
//
// Declare each dependency as a managed resource; keep the returned cleanup to
// register with a shutdown coordinator (see the closer package):
//
//	store, cleanup := dim.NewResource("Store",
//	    func(ctx context.Context) (Store, dim.CleanupFunc, error) {
//	        st, err := postgres.New(ctx, dsn)
//	        if err != nil {
//	            return nil, nil, err
//	        }
//	        return st, func() error { return st.Close() }, nil
//	    })
//	closer.Add(cleanup)
//
//	db := store(ctx) // built (and logged) on first call, cached thereafter
//
// For a value with no cleanup, wrap a factory in a Provider directly. A factory
// error is fatal and panics, so initialization failures surface at startup:
//
//	cfg := dim.OnceWithName("Config", func(ctx context.Context) (Config, error) {
//	    return loadConfig(ctx)
//	})
//
// # API
//
//   - Provider[T]: lazy resource accessor func(ctx) T.
//   - FactoryFunc[T]: func(ctx) (T, CleanupFunc, error) used by NewResource.
//   - CleanupFunc: a teardown func() error, run LIFO at shutdown.
//   - Once / OnceWithName: memoize a factory into a Provider (panic on error).
//   - NewResource: a memoized Provider plus an auto-registered CleanupFunc.
//   - NamedCleanup: wrap a teardown with named, timed logging.
package dim
