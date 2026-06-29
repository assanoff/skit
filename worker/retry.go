package worker

import (
	"context"

	"github.com/assanoff/skit/retry"
)

// Retry wraps h so a failed Handle is retried in-process per cfg before the
// error propagates to the Processor (which then records the outcome via its
// Sink). The whole retry runs inside a single Source lease, so keep cfg's
// budget and delays well under the lease/sweep timeout, or another consumer may
// reclaim the item while this one is still retrying.
//
// It is a thin convenience over retry.Do for the common case of adding
// in-process retry to a Processor's Handler; for non-Handler operations call
// retry.Do directly.
func Retry[T any](cfg retry.Config, h Handler[T]) Handler[T] {
	return HandlerFunc[T](func(ctx context.Context, item T) error {
		return retry.Do(ctx, cfg, func(ctx context.Context) error {
			return h.Handle(ctx, item)
		})
	})
}
