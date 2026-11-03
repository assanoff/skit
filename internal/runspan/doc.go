// Package runspan records a run of periodic work — a worker tick, a cron run —
// as the root span of its own trace, for worker and cron.
//
//	ctx, span := runspan.Start(ctx, tracer, name)
//	var err error
//	finished := false // stays false when run panics
//	defer func() { runspan.End(span, finished, err) }()
//	err = run(ctx)
//	finished = true
//
// Start lifts otel.Suppress from the context, so a run that asked for a span
// gets one even when it was started from a suppressed context. End marks a run
// that did not finish as a panic and a run with an error as failed.
package runspan
