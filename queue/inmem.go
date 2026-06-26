package queue

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/assanoff/skit/worker"
)

// InMem is an in-memory Queue with the same lease/retry/dedup semantics as PG,
// for unit tests and local runs without a database. It is safe for concurrent
// use. It is not durable and does not coordinate across processes.
type InMem struct {
	mu           sync.Mutex
	nextID       int64
	tasks        map[int64]*Task
	byName       map[string]int64
	leasedAt     map[int64]time.Time
	leaseTimeout time.Duration
}

var (
	_ Queue               = (*InMem)(nil)
	_ worker.Source[Task] = (*InMem)(nil)
	_ worker.Sink[Task]   = (*InMem)(nil)
)

// NewInMem builds an in-memory queue. leaseTimeout defaults to 5m when <= 0.
func NewInMem(leaseTimeout time.Duration) *InMem {
	if leaseTimeout <= 0 {
		leaseTimeout = 5 * time.Minute
	}
	return &InMem{
		tasks:        map[int64]*Task{},
		byName:       map[string]int64{},
		leasedAt:     map[int64]time.Time{},
		leaseTimeout: leaseTimeout,
	}
}

// Schedule implements Queue.
func (q *InMem) Schedule(_ context.Context, p ScheduleParams) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	name := p.Name
	if name == "" {
		name = uuid.NewString()
	}
	if _, dup := q.byName[name]; dup {
		return false, nil
	}

	now := time.Now().UTC()
	q.nextID++
	id := q.nextID
	q.tasks[id] = &Task{
		ID:        id,
		Name:      name,
		Kind:      p.Kind,
		Payload:   p.Payload,
		CreatedAt: now,
		RunAt:     now.Add(p.Delay),
	}
	q.byName[name] = id
	return true, nil
}

// Claim implements Queue.
func (q *InMem) Claim(_ context.Context, now time.Time, limit int) ([]Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Collect ready, unleased (or lease-expired) tasks in run_at, id order.
	var ready []*Task
	for _, t := range q.tasks {
		if !t.RunAt.After(now) && q.claimable(t, now) {
			ready = append(ready, t)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		if ready[i].RunAt.Equal(ready[j].RunAt) {
			return ready[i].ID < ready[j].ID
		}
		return ready[i].RunAt.Before(ready[j].RunAt)
	})
	if limit > 0 && len(ready) > limit {
		ready = ready[:limit]
	}

	out := make([]Task, 0, len(ready))
	for _, t := range ready {
		t.LeaseID = uuid.NewString()
		t.Attempts++
		q.leasedAt[t.ID] = now
		out = append(out, *t) // copy so callers cannot mutate queue state
	}
	return out, nil
}

// claimable reports whether t currently holds no live lease.
func (q *InMem) claimable(t *Task, now time.Time) bool {
	if t.LeaseID == "" {
		return true
	}
	leasedAt := q.leasedAt[t.ID]
	return leasedAt.Add(q.leaseTimeout).Before(now) // lease expired
}

// MarkDone implements Queue.
func (q *InMem) MarkDone(_ context.Context, t Task, _ time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	cur, ok := q.tasks[t.ID]
	if !ok || cur.LeaseID != t.LeaseID {
		return ErrLeaseLost
	}
	delete(q.tasks, t.ID)
	delete(q.byName, cur.Name)
	delete(q.leasedAt, t.ID)
	return nil
}

// MarkFailed implements Queue.
func (q *InMem) MarkFailed(_ context.Context, t Task, errMsg string, terminal bool, now time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	cur, ok := q.tasks[t.ID]
	if !ok || cur.LeaseID != t.LeaseID {
		return ErrLeaseLost
	}
	cur.LastError = errMsg
	if terminal {
		// Park as a dead letter: keep the row, drop the lease, mark done so it is
		// never reclaimed. Modeled via a sentinel: clear lease and set RunAt far
		// future so claimable never picks it.
		cur.LeaseID = ""
		delete(q.leasedAt, cur.ID)
		cur.RunAt = deadLetterTime
		return nil
	}
	// Retryable: release the lease and make it immediately claimable again.
	cur.LeaseID = ""
	delete(q.leasedAt, cur.ID)
	cur.RunAt = now
	return nil
}

// deadLetterTime parks terminally-failed tasks so Claim never selects them while
// keeping the row inspectable in tests.
var deadLetterTime = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// Len reports the number of tasks still in the queue (including dead letters).
// Test helper.
func (q *InMem) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}
