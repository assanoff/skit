package queue

import (
	"database/sql"
	"time"
)

// leased is a task under the lease of the claim that returned it: the item
// the processor of a queue claims, handles and marks.
type leased struct {
	Task

	leaseID string
}

// taskRow is the persistence model for a queue_tasks row. The nullable columns
// (key, lease_id, last_error) map to sql.Null* wrappers; the rest map directly.
// It is deliberately separate from the core Task: Task is the pure type
// handlers see, taskRow owns the db tags and NULL handling, and toLeased
// bridges the two.
type taskRow struct {
	ID        int64          `db:"id"`
	Queue     string         `db:"queue"`
	Kind      string         `db:"kind"`
	Key       sql.NullString `db:"key"`
	Payload   []byte         `db:"payload"`
	CreatedAt time.Time      `db:"created_at"`
	RunAt     time.Time      `db:"run_at"`
	Attempts  int            `db:"attempts"`
	LeaseID   sql.NullString `db:"lease_id"`
	LastError sql.NullString `db:"last_error"`
}

// toLeased converts a claimed row into a leased task. NULL columns become zero
// values; timestamps are normalized to UTC.
func toLeased(r taskRow) leased {
	//exhaustruct:enforce
	return leased{
		ID:        r.ID,
		Queue:     r.Queue,
		Kind:      r.Kind,
		Key:       r.Key.String,
		Payload:   r.Payload,
		CreatedAt: r.CreatedAt.UTC(),
		RunAt:     r.RunAt.UTC(),
		Attempts:  r.Attempts,
		LastError: r.LastError.String,
		leaseID:   r.LeaseID.String,
	}
}

func toLeasedAll(rows []taskRow) []leased {
	out := make([]leased, len(rows))
	for i := range rows {
		out[i] = toLeased(rows[i])
	}
	return out
}
