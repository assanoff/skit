package worker

import "context"

// Runnable is a supervised unit of work. Start blocks until ctx is canceled or
// the unit fails; Stop performs a graceful shutdown. Name identifies it in logs.
type Runnable interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Name() string
}

// RunnableFunc adapts a plain start function (plus name and optional stop) into
// a Runnable, so callers can supply behavior as functions instead of types.
type RunnableFunc struct {
	NameValue string
	StartFn   func(ctx context.Context) error
	StopFn    func(ctx context.Context) error
}

// Name implements Runnable.
func (r RunnableFunc) Name() string { return r.NameValue }

// Start implements Runnable.
func (r RunnableFunc) Start(ctx context.Context) error {
	if r.StartFn == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	return r.StartFn(ctx)
}

// Stop implements Runnable.
func (r RunnableFunc) Stop(ctx context.Context) error {
	if r.StopFn == nil {
		return nil
	}
	return r.StopFn(ctx)
}
