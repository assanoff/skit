// Package closer manages graceful shutdown of resources. Cleanup handlers run
// in LIFO order (last registered, first closed), matching resource dependency
// order.
//
// It exposes both a process-global default (Add/AddNamed/Close/CloseSync) for
// the common single-lifecycle application, and an instance API (New /
// NewWithWait) for callers that need an isolated lifecycle. NewWithWait also
// installs a signal handler so Wait blocks until SIGINT/SIGTERM (or a custom
// set) arrives.
//
// # Usage
//
// Register teardowns as resources are created, wait for a shutdown signal, then
// close everything in reverse:
//
//	cl := closer.NewWithWait() // listens for DefaultSignals
//
//	srv := startServer()
//	cl.AddNamed("http", func() error { return srv.Shutdown(context.Background()) })
//	cl.AddNamed("db", db.Close)
//
//	cl.Wait()              // block until a signal arrives
//	if err := cl.CloseSync(); err != nil { // LIFO, ordered
//	    log.Error("shutdown", "err", err)
//	}
//
// The package-level Add/Close functions operate on a shared global Closer for
// apps that only need one lifecycle:
//
//	closer.AddNamed("db", db.Close)
//	defer closer.CloseSync()
//
// # Close vs CloseSync
//
// CloseSync runs handlers sequentially in strict LIFO order — use it when
// teardown order matters (it usually does, mirroring construction order).
// Close runs handlers concurrently with no order guarantee, for independent
// resources. Both are idempotent, recover panics in handlers, and join all
// errors. Handlers added after a close are ignored.
//
// # Options
//
//   - DefaultSignals: the signal set NewWithWait listens for when none are
//     passed (SIGINT, SIGTERM, SIGQUIT, SIGHUP).
//   - NewWithWait(sigs ...os.Signal): override the signal set.
package closer
