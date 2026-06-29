package queue

import (
	"context"

	"github.com/assanoff/skit/retry"
)

// Retry wraps fn so transient failures are retried in-process per cfg before the
// task is marked failed. It is a thin convenience over retry.Do for JobFunc
// handlers (register the wrapped func on a Mux).
//
// The retry runs inside a single claim, so the task's lease is held for the
// whole duration — keep cfg's budget and delays well under the queue's
// LeaseTimeout, or another consumer may reclaim the task while it is still
// retrying. For long, durable backoff that survives crashes and frees the
// worker between attempts, rely on the store's own retry (a non-terminal
// MarkFailed reschedules the task) instead of a long in-process budget.
func Retry(cfg retry.Config, fn JobFunc) JobFunc {
	return func(ctx context.Context, t Task) error {
		return retry.Do(ctx, cfg, func(ctx context.Context) error {
			return fn(ctx, t)
		})
	}
}
