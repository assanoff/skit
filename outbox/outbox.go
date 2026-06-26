package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Event statuses stored in the status column.
const (
	StatusPending  = "pending"   // awaits the relay
	StatusInFlight = "in_flight" // leased by a relay, publish in progress
	StatusSent     = "sent"      // terminal: published successfully
	StatusFailed   = "failed"    // terminal: retry budget exhausted
)

// DefaultMaxAttempts is the per-row retry budget assigned by NewEvent.
const DefaultMaxAttempts = 10

// ErrLeaseLost is returned by MarkSent/MarkFailed when the row's lease no longer
// matches the caller's lease id — the sweeper or another relay reclaimed it. The
// mark is a no-op; the row will be reprocessed under its new lease.
var ErrLeaseLost = errors.New("outbox: lease lost")

// Event is an outbox record as seen by callers — a pure core type with no db
// tags or Null wrappers (the PG store owns that mapping). Payload is the
// business data only; the broker.Publisher wraps it in a CloudEvents envelope.
//
// Routing is transport-neutral: Topic is the logical destination and Key an
// optional routing/ordering key. Each broker maps them to its own concepts
// (RabbitMQ exchange+routing key, Kafka topic+partition key, NATS subject), so
// the same event row can be delivered over any transport.
type Event struct {
	ID            uuid.UUID
	Type          string
	ContentType   string
	Topic         string
	Key           string
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
// logical destination (required); key is the optional routing/ordering key.
// headers may be nil (normalized to "{}"). payload must already be serialized
// by the caller.
//
// Most producers don't call NewEvent directly — they Publish a typed value
// through a tx-bound Publisher (see Bind / WithinTran), which resolves the
// route from a Registry. NewEvent is the low-level escape hatch for building a
// row with a dynamic topic.
func NewEvent(eventType, topic, key, contentType string, payload []byte, headers map[string]any) (Event, error) {
	switch {
	case eventType == "":
		return Event{}, fmt.Errorf("outbox new event: type is required")
	case topic == "":
		return Event{}, fmt.Errorf("outbox new event: topic is required")
	case contentType == "":
		return Event{}, fmt.Errorf("outbox new event: content_type is required")
	case len(payload) == 0:
		return Event{}, fmt.Errorf("outbox new event: payload is required")
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
		Key:           key,
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
	WithTx(tx sqlx.ExtContext) Store

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

	// MarkFailed transitions in_flight -> pending (attempts+1 < max, with
	// next_attempt_at advanced by backoff) or -> failed (attempts+1 >= max).
	// Guarded by leaseID; returns ErrLeaseLost on a lease mismatch.
	MarkFailed(ctx context.Context, ev Event, leaseID uuid.UUID, errMsg string, now time.Time) error

	// SweepExpiredLeases returns in_flight rows whose lease is older than
	// leaseTimeout back to pending (a crashed-relay signal — attempts is NOT
	// incremented). Returns the number reclaimed.
	SweepExpiredLeases(ctx context.Context, leaseTimeout time.Duration, now time.Time, limit int) (int64, error)

	// Cleanup deletes sent/failed rows created before now-retention. Returns the
	// number of rows deleted.
	Cleanup(ctx context.Context, retention time.Duration, now time.Time) (int64, error)
}
