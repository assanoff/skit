package app

import (
	"github.com/assanoff/skit/closer"
	"github.com/assanoff/skit/dim"
)

// Initializer builds and assigns one dependency onto the container D, returning
// an optional cleanup. It is the unit of the assembly pattern: a Deps container
// of lazy dim.Provider fields plus an ordered slice of Initializers. A typical
// initializer assigns a provider and returns the resource's cleanup:
//
//	func initStore(d *Deps) (dim.CleanupFunc, error) {
//		d.DB = dim.NewResource("DB", provider.Postgres(&d.Opts, d.Logger))
//		return nil, nil // resource cleanup is returned by NewResource via the provider
//	}
//
// D is the application's own dependency container — the SDK stays agnostic of
// its shape.
type Initializer[D any] func(*D) (dim.CleanupFunc, error)

// InitDeps runs each initializer in order against d, registering every non-nil
// cleanup with the global closer so resources are released in LIFO order on
// shutdown (pair it with a deferred closer.CloseSync in your serve command). It
// stops at the first error and returns it; cleanups already registered still run
// when the caller closes.
//
// This is the reusable form of the per-app InitDeps loop: declare a
// []app.Initializer[Deps] and call app.InitDeps(&deps, inits).
func InitDeps[D any](d *D, inits []Initializer[D]) error {
	return initDeps(d, inits, closer.Add)
}

// initDeps is the registrar-injectable core, kept separate so tests can observe
// cleanup registration without touching the global closer.
func initDeps[D any](d *D, inits []Initializer[D], register func(func() error)) error {
	for _, fn := range inits {
		cleanup, err := fn(d)
		if err != nil {
			return err
		}
		if cleanup != nil {
			register(cleanup)
		}
	}
	return nil
}
