package outbox

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// rowDB is the persistence model for an outbox_events row. Nullable columns map
// to sql.Null* wrappers / uuid.NullUUID; non-null timestamps map directly to
// time.Time. Payload and Headers are byte blobs (BYTEA / JSONB).
//
// It is deliberately separate from the core Event (outbox.go): Event is the
// pure type callers see, rowDB owns the db tags and NULL handling. The two are
// bridged by toCoreRow / toDBRow so neither leaks into the other.
type rowDB struct {
	ID            uuid.UUID      `db:"id"`
	Type          string         `db:"type"`
	ContentType   string         `db:"content_type"`
	Topic         string         `db:"topic"`
	Key           string         `db:"route_key"`
	Payload       []byte         `db:"payload"`
	Headers       []byte         `db:"headers"`
	Status        string         `db:"status"`
	Attempts      int            `db:"attempts"`
	MaxAttempts   int            `db:"max_attempts"`
	LastError     sql.NullString `db:"last_error"`
	NextAttemptAt time.Time      `db:"next_attempt_at"`
	CreatedAt     time.Time      `db:"created_at"`
	SentAt        sql.NullTime   `db:"sent_at"`
	LeasedAt      sql.NullTime   `db:"leased_at"`
	LeaseID       uuid.NullUUID  `db:"lease_id"`
}

// toDBRow converts a core Event into its persistence model. Timestamps are
// coerced to UTC; zero values for the nullable fields map to NULL.
func toDBRow(e Event) rowDB {
	return rowDB{
		ID:            e.ID,
		Type:          e.Type,
		ContentType:   e.ContentType,
		Topic:         e.Topic,
		Key:           e.Key,
		Payload:       e.Payload,
		Headers:       e.Headers,
		Status:        e.Status,
		Attempts:      e.Attempts,
		MaxAttempts:   e.MaxAttempts,
		LastError:     sql.NullString{String: e.LastError, Valid: e.LastError != ""},
		NextAttemptAt: e.NextAttemptAt.UTC(),
		CreatedAt:     e.CreatedAt.UTC(),
		SentAt:        sql.NullTime{Time: e.SentAt, Valid: !e.SentAt.IsZero()},
		LeasedAt:      sql.NullTime{Time: e.LeasedAt, Valid: !e.LeasedAt.IsZero()},
		LeaseID:       uuid.NullUUID{UUID: e.LeaseID, Valid: e.LeaseID != uuid.Nil},
	}
}

// toCoreRow converts a DB row back into a core Event. NULL columns become zero
// values ("" for LastError, time.Time{} for SentAt/LeasedAt, uuid.Nil for
// LeaseID); timestamps are normalized to UTC.
func toCoreRow(r rowDB) Event {
	e := Event{
		ID:            r.ID,
		Type:          r.Type,
		ContentType:   r.ContentType,
		Topic:         r.Topic,
		Key:           r.Key,
		Payload:       r.Payload,
		Headers:       r.Headers,
		Status:        r.Status,
		Attempts:      r.Attempts,
		MaxAttempts:   r.MaxAttempts,
		LastError:     r.LastError.String,
		NextAttemptAt: r.NextAttemptAt.UTC(),
		CreatedAt:     r.CreatedAt.UTC(),
	}
	if r.SentAt.Valid {
		e.SentAt = r.SentAt.Time.UTC()
	}
	if r.LeasedAt.Valid {
		e.LeasedAt = r.LeasedAt.Time.UTC()
	}
	if r.LeaseID.Valid {
		e.LeaseID = r.LeaseID.UUID
	}
	return e
}

func toCoreRows(rows []rowDB) []Event {
	out := make([]Event, len(rows))
	for i := range rows {
		out[i] = toCoreRow(rows[i])
	}
	return out
}
