package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/assanoff/skit/dbx"
)

// Event statuses stored in the status column.
const (
	StatusPending  = "pending"   // awaits the relay
	StatusInFlight = "in_flight" // leased by a relay, publish in progress
	StatusSent     = "sent"      // terminal: published successfully
	StatusFailed   = "failed"    // terminal: the broker rejected it for good
)

// DefaultMaxAttempts is the max_attempts NewEvent stamps on a row. The relay
// does not limit attempts: it retries until the event goes out, and only an
// error marked broker.Permanent moves the event to failed.
const DefaultMaxAttempts = 10

// ErrLeaseLost is returned by MarkSent/MarkFailed when the row's lease no longer
// matches the caller's lease id — the sweeper or another relay reclaimed it. The
// mark is a no-op; the row will be reprocessed under its new lease.
var ErrLeaseLost = errors.New("outbox: lease lost")

// Event is an outbox record as seen by callers — a pure core type with no db
// tags or Null wrappers (the PG store owns that mapping). Payload is the
// business data only; the broker.Publisher wraps it in its envelope.
//
// Topic is the transport-neutral destination. A key within it (a RabbitMQ
// routing key, a Kafka partition key) is the transport's concern: the
// publisher of the adapter derives it from the message, so the same event row
// can be delivered over any transport.
type Event struct {
	ID            uuid.UUID
	Type          string
	ContentType   string
	Topic         string
	Payload       []byte
	Headers       []byte // JSON object of transport headers ("{}" when none)
	Status        string
	Attempts      int
	MaxAttempts   int
	LastError     string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	SentAt        time.Time
	LeasedAt      time.Time
	LeaseID       uuid.UUID
}

// NewEvent builds a pending Event ready for Insert, stamping a fresh id, UTC
// timestamps, attempts=0, and max_attempts=DefaultMaxAttempts. topic is the
// logical destination (required). headers may be nil (normalized to "{}").
// payload must already be serialized by the caller.
//
// Most producers don't call NewEvent directly — they Publish a typed value
// through a tx-bound Publisher (see Bind / WithinTran), which resolves the
// route from a Registry. NewEvent is the low-level escape hatch for building a
// row with a dynamic topic.
func NewEvent(eventType, topic, contentType string, payload []byte, headers map[string]any) (Event, error) {
	switch {
	case eventType == "":
		return Event{}, errors.New("outbox new event: type is required")
	case topic == "":
		return Event{}, errors.New("outbox new event: topic is required")
	case contentType == "":
		return Event{}, errors.New("outbox new event: content_type is required")
	case len(payload) == 0:
		return Event{}, errors.New("outbox new event: payload is required")
	}

	headersJSON := []byte("{}")
	if headers != nil {
		b, err := json.Marshal(headers)
		if err != nil {
			return Event{}, fmt.Errorf("outbox new event: marshal headers: %w", err)
		}
		headersJSON = b
	}

	now := time.Now().UTC()
	return Event{
		ID:            uuid.New(),
		Type:          eventType,
		ContentType:   contentType,
		Topic:         topic,
		Payload:       payload,
		Headers:       headersJSON,
		Status:        StatusPending,
		MaxAttempts:   DefaultMaxAttempts,
		NextAttemptAt: now,
		CreatedAt:     now,
	}, nil
}

// Store is the persistence port for the outbox table. The Postgres
// implementation is *PG.
type Store interface {
	// WithTx returns a sibling Store whose queries run on tx. Used by WithinTran
	// to insert events in the caller's domain transaction.
	WithTx(tx dbx.Querier) Store

	// Insert writes pending events. Bound to a transaction when the Store was
	// derived via WithTx.
	Insert(ctx context.Context, events ...Event) error

	// LeasePending atomically claims up to limit pending events whose
	// next_attempt_at <= now, marks them in_flight with a fresh lease id, and
	// returns them (FOR UPDATE SKIP LOCKED, so relay replicas split work).
	LeasePending(ctx context.Context, now time.Time, limit int) ([]Event, error)

	// MarkSent transitions in_flight -> sent. Guarded by leaseID; returns
	// ErrLeaseLost on a lease mismatch.
	MarkSent(ctx context.Context, ev Event, leaseID uuid.UUID, now time.Time) error

	// MarkFailed transitions in_flight -> failed when terminal, otherwise ->
	// pending with next_attempt_at advanced by backoff; attempts grows either
	// way, with no limit. Guarded by leaseID; returns ErrLeaseLost on a lease
	// mismatch.
	MarkFailed(ctx context.Context, ev Event, leaseID uuid.UUID, errMsg string, terminal bool, now time.Time) error

	// Release returns the rows still in_flight under leaseID to pending
	// untouched: attempts and next_attempt_at stay as they were. The relay
	// releases the rest of a batch it stopped at a failed publish. Returns the
	// number released.
	Release(ctx context.Context, leaseID uuid.UUID) (int64, error)

	// SweepExpiredLeases returns in_flight rows whose lease is older than
	// leaseTimeout back to pending (a crashed-relay signal — attempts is NOT
	// incremented). Returns the number reclaimed.
	SweepExpiredLeases(ctx context.Context, leaseTimeout time.Duration, now time.Time, limit int) (int64, error)

	// Cleanup deletes sent/failed rows created before now-retention. Returns the
	// number of rows deleted.
	Cleanup(ctx context.Context, retention time.Duration, now time.Time) (int64, error)
}
