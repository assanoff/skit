package queue

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"
)

// Task is a unit of queued work as its handler sees it. Payload is opaque to
// the queue: the handler decodes it, typically through Decode.
type Task struct {
	ID        int64
	Queue     string
	Kind      string
	Key       string // dedup key within Queue; "" — none
	Payload   []byte
	CreatedAt time.Time
	RunAt     time.Time
	Attempts  int    // failed attempts before this one
	LastError string // error of the last failed attempt
}

// Params describes a task to schedule.
type Params struct {
	// Queue is the queue the task goes to; required.
	Queue string
	// Kind selects the handler of the task within its queue; required.
	Kind string
	// Key deduplicates the task within Queue: while a task with the same Key
	// is in the queue — waiting, leased or dead — Schedule inserts nothing
	// and reports false. "" schedules every call.
	Key string
	// Payload is the opaque body of the task.
	Payload []byte
	// Delay postpones the task: it runs not before now + Delay.
	Delay time.Duration
}

// JobFunc handles one task. Its error decides the outcome of the task: nil
// removes it; an error marked by Permanent dead-letters it; one marked by
// Halt retries it with backoff and stops the batch, whose rest goes back
// untouched; any other error retries the task with backoff.
type JobFunc func(ctx context.Context, t Task) error

// permanentError marks a failure no retry fixes; see Permanent.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }

func (e permanentError) Unwrap() error { return e.err }

// Permanent marks err as a failure no retry fixes, such as a payload that does
// not decode: the task goes to the dead letters of its queue. Permanent(nil) is
// nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// haltError marks a failure that stops the batch; see Halt.
type haltError struct{ err error }

func (e haltError) Error() string { return e.err.Error() }

func (e haltError) Unwrap() error { return e.err }

// Halt marks err as a failure of a dependency every task of the batch needs,
// such as a broker that is down: the task is retried with backoff, and the
// batch stops — its rest goes back untouched, with no attempt counted, and
// the queue polls again after Config.Poll. Halt(nil) is nil.
func Halt(err error) error {
	if err == nil {
		return nil
	}
	return haltError{err: err}
}

// isPermanent reports whether err, or an error it wraps, was marked by
// Permanent.
func isPermanent(err error) bool {
	_, ok := errors.AsType[permanentError](err)
	return ok
}

// isHalt reports whether err, or an error it wraps, was marked by Halt.
func isHalt(err error) bool {
	_, ok := errors.AsType[haltError](err)
	return ok
}

// Decode adapts fn to a JobFunc that decodes the JSON Payload of a task into
// a T first. A payload that does not decode is Permanent: it never will.
func Decode[T any](fn func(ctx context.Context, t Task, v T) error) JobFunc {
	return func(ctx context.Context, t Task) error {
		var v T
		if err := json.Unmarshal(t.Payload, &v); err != nil {
			return Permanent(fmt.Errorf("queue: decode %s: %w", t.Kind, err))
		}
		return fn(ctx, t, v)
	}
}
