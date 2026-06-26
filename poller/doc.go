// Package poller maintains a single "current value" that is refreshed by
// periodically calling a Getter.
//
// It is the read-side counterpart to worker.Loop: instead of performing work on
// each tick, it caches the latest successfully fetched value so hot paths can
// read it (via Current) without blocking on the source. A failed poll leaves
// the cached value unchanged. Typical uses are feature flags, dynamic config,
// exchange rates, or any slowly-changing remote value callers want cheaply and
// concurrently. A Poller is a worker.Runnable, so it can be supervised in a
// worker.Group alongside servers and other workers.
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
//	    OnError:     func(err error) { metrics.PollFail.Inc() },
//	})
//
//	g.Add(p) // worker.Group supervises Start/Stop
//
//	rates := p.Current() // latest successful value (the initial until first poll)
//
// # Config
//
//   - Name: identifies the poller in logs and panic metrics (default "poller").
//   - Interval: time between polls (required, > 0).
//   - PollTimeout: bounds each Getter call (0 = inherit the run context).
//   - OnError: invoked with each failed poll's error; may be nil.
//   - OnPanic: safetick.PanicHandler invoked when a poll panics; may be nil.
package poller
