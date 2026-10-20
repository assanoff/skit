// Package poller maintains a single "current value" that is refreshed by
// periodically calling a Getter.
//
// It is the read-side counterpart to worker.Loop: instead of performing work on
// each tick, it caches the latest successfully fetched value so hot paths can
// read it (via Current) without blocking on the source. A failed poll leaves
// the cached value unchanged. Typical uses are feature flags, dynamic config,
// exchange rates, or any slowly-changing remote value callers want cheaply and
// concurrently. A Poller is a worker.Runnable, so it can be supervised in a
// worker.Group alongside servers and other workers. Start runs the polls under
// otel.Suppress, so they export no traces; a direct Poll keeps the trace of its
// caller.
//
// The Getter gets the run context (bounded by PollTimeout, when set) and must
// return once it is canceled: Start does not return before the running Getter
// does, so a Getter that ignores its context holds the shutdown of the group up
// to its ShutdownTimeout.
//
// # Usage
//
// Seed an initial value, supervise the poller, and read the cached value
// concurrently:
//
//	p := poller.New(log, defaultRates, func(ctx context.Context) (Rates, error) {
//	    return fetchRates(ctx)
//	}, poller.Config{
//	    Name:        "rates",
//	    Interval:    30 * time.Second,
//	    PollTimeout: 5 * time.Second,
//	}, poller.WithOnError(func(error) { pollFailures.Inc() }))
//
//	g.Add(p) // worker.Group supervises Start/Stop
//
//	rates := p.Current() // latest successful value (the initial until first poll)
//
// # Config
//
//   - Name: identifies the poller in logs (default "poller").
//   - Interval: time between polls (default 1s).
//   - PollTimeout: bounds each Getter call (0 = inherit the run context).
//
// # Options
//
//   - WithOnError(h): invoked with the error of each failed poll.
//   - WithOnPanic(h): a safetick.PanicHandler invoked when a poll panics.
package poller
