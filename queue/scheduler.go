package queue

import (
	"context"
	"fmt"

	"github.com/assanoff/skit/dbx"
)

// Scheduler schedules tasks in the transaction it is bound to (Bind). The
// domain declares its own interface with the same method — Schedule(ctx,
// v any) error — and *Scheduler satisfies it without an adapter, so the domain
// imports neither queue nor dbx and never names a queue.
type Scheduler struct {
	tx  *dbx.Tx
	q   *PG
	reg *Registry
}

// Bind returns a Scheduler whose Schedule writes tasks into q on tx, routing
// each value by reg. The tasks commit or roll back with the other writes in
// tx: no task runs for a change that rolled back, and none is lost for one
// that committed. A pool in place of tx does not compile; PG.Schedule
// schedules on the pool.
func Bind(tx *dbx.Tx, q *PG, reg *Registry) *Scheduler {
	return &Scheduler{tx: tx, q: q, reg: reg}
}

// Schedule writes a task for v within the bound transaction: the Registry
// resolves the queue and kind of the type of v, and the Encoder of its route
// writes the Payload. A type registered with Drop writes nothing and returns
// nil. An unregistered type, a nil v or a failed encoding is an error, and the
// transaction stays usable: nothing was written.
func (s *Scheduler) Schedule(ctx context.Context, v any) error {
	rt, err := s.reg.lookup(v)
	if err != nil {
		return err
	}
	if rt.drop {
		return nil
	}
	payload, err := rt.enc.Encode(ctx, v)
	if err != nil {
		return fmt.Errorf("queue schedule %s: encode: %w", rt.kind, err)
	}
	p := Params{Queue: rt.queue, Kind: rt.kind, Payload: payload}
	_, err = s.q.insert(ctx, s.tx, p)
	return err
}
