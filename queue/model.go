package queue

import (
	"database/sql"
	"time"
)

// taskRow is the persistence model for a queue_tasks row. The nullable columns
// (lease_id, last_error) map to sql.Null* wrappers; the rest map directly. It
// is deliberately separate from the core Task (queue.go): Task is the pure type
// consumers see, taskRow owns the db tags and NULL handling. toTask bridges the
// two so neither leaks into the other.
type taskRow struct {
	ID        int64          `db:"id"`
	Name      string         `db:"name"`
	Kind      string         `db:"kind"`
	Payload   []byte         `db:"payload"`
	CreatedAt time.Time      `db:"created_at"`
	RunAt     time.Time      `db:"run_at"`
	Attempts  int            `db:"attempts"`
	LeaseID   sql.NullString `db:"lease_id"`
	LastError sql.NullString `db:"last_error"`
}

// toTask converts a DB row into a core Task. NULL columns become zero values
// ("" for LeaseID/LastError); timestamps are normalized to UTC.
func toTask(r taskRow) Task {
	return Task{
		ID:        r.ID,
		Name:      r.Name,
		Kind:      r.Kind,
		Payload:   r.Payload,
		CreatedAt: r.CreatedAt.UTC(),
		RunAt:     r.RunAt.UTC(),
		Attempts:  r.Attempts,
		LeaseID:   r.LeaseID.String,
		LastError: r.LastError.String,
	}
}

func toTasks(rows []taskRow) []Task {
	out := make([]Task, len(rows))
	for i := range rows {
		out[i] = toTask(rows[i])
	}
	return out
}
