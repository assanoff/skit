// Package app holds the application assembly layer of the SDK. It provides the
// reusable pieces of the "build & run" pattern — a deps/provider/server
// assembly layout — so an application does not re-implement them.
//
// The pattern: a Deps container of lazy dim.Provider fields, an ordered slice of
// Initializers (infrastructure -> core -> handlers -> workers), and InitDeps
// which runs each initializer and registers its cleanup with the global closer
// for LIFO shutdown. Deps itself is application-specific; the SDK supplies the
// generic InitDeps over it.
//
// # Usage
//
//	// deps.go (in your application)
//	var Initializers = []app.Initializer[Deps]{
//		initTracer, initStore, initBroker, // infrastructure
//		initWidgetCore,                    // core
//		initWidgetHandler,                 // handlers
//	}
//
//	// serve command
//	d := &Deps{Opts: cfg, Logger: log}
//	if err := app.InitDeps(d, Initializers); err != nil {
//		return err
//	}
//	defer func() { _ = closer.CloseSync() }() // LIFO release of every cleanup
//
// Each initializer typically assigns a dim.Provider built from a provider
// factory (value, cleanup, error) and returns that cleanup, so deps declares
// WHAT to build and provider declares HOW.
//
// # Commands and partial dependency sets
//
// The runnable is not always a server (or group of servers): a CLI command —
// e.g. connect to the DB, run a business function, publish to a queue — also
// needs dependencies, sometimes only a subset. Two facts make this easy:
//
//   - dim providers are LAZY: an initializer only wires a provider and registers
//     its (no-op-until-built) cleanup; the resource is constructed on first
//     d.X(ctx). So you can run the FULL Initializers slice and only the
//     dependencies the command actually touches get built (and cleaned up) —
//     handlers, consumers and idle infra never connect.
//
//   - Initializers is just data. A command that must NOT touch some
//     infrastructure (or wants to guarantee only certain resources connect)
//     passes a SUBSET slice instead. Compose subsets from named groups with the
//     standard library:
//
//     var (
//     Infra    = []app.Initializer[Deps]{initTracer, initStore, initQueue}
//     Core     = []app.Initializer[Deps]{initWidgetCore}
//     Handlers = []app.Initializer[Deps]{initWidgetHandler, initWidgetGRPC}
//     )
//     // serve: everything.
//     _ = app.InitDeps(d, slices.Concat(Infra, Core, Handlers))
//     // a "drain queue" command: just store + queue + core, no handlers.
//     _ = app.InitDeps(d, slices.Concat(Infra, Core))
//
// # API
//
//   - Initializer[D]: func(*D) (dim.CleanupFunc, error) — builds one dependency.
//   - InitDeps[D]: run a []Initializer[D], registering cleanups with the global
//     closer; stops at the first error.
//   - Run: supervise runnables (a server.Set) until a signal or failure, then
//     graceful shutdown + closer release. The serve-command entry point.
//   - RunCommand[D]: bootstrap a one-shot CLI command — init a subset of deps,
//     run fn with a signal-cancelable context, release resources after.
package app
