package auditlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/logger"
)

// Recorder records an audit entry best-effort (errors are handled internally, not
// returned). Transports (auditrest, auditgrpc, auditbus) depend on this interface
// so the recording can be synchronous (*Core) or asynchronous (AsyncRecorder, or
// a queue-backed recorder) without changing the call sites.
type Recorder interface {
	Record(ctx context.Context, entry NewAuditLog)
}

// ErrNotFound is returned when no audit log entry matches the query.
var ErrNotFound = errors.New("audit log not found")

// ErrInvalidFilter is returned when a query filter is missing required fields
// (e.g. a diff request without both version numbers).
var ErrInvalidFilter = errors.New("invalid audit log query filter")

// Core is the audit-log business logic: it records versioned model snapshots and
// answers history/diff queries. It depends only on the Store interface declared
// here; the Postgres implementation lives in the db subpackage.
type Core struct {
	log   *logger.Logger
	store Store

	// Opportunistic compaction: when autoCompactEvery > 0, Create compacts the
	// model it just wrote once every autoCompactEvery versions, so history is kept
	// bounded inline without a separate sweep. The check is gated by version number
	// so most writes pay nothing; Compact itself no-ops when below its MinVersions.
	autoCompactEvery int
	autoCompactOpts  CompactOptions
}

// Option customizes a Core.
type Option func(*Core)

// WithAutoCompact enables opportunistic compaction: after Create stores a new
// version, once every `every` versions it compacts that model with opts
// (best-effort — a failure is logged, never returned). Set every to roughly the
// threshold at which history should be thinned. The separate Compact/CompactBatch
// remain available for on-demand or scheduled runs.
func WithAutoCompact(every int, opts CompactOptions) Option {
	return func(c *Core) {
		c.autoCompactEvery = every
		c.autoCompactOpts = opts
	}
}

//go:generate go run github.com/matryer/moq@latest -out mocks/auditlogdb.go -pkg mocks . Store

// Store is the persistence contract for audit log entries. WithTx returns a
// sibling bound to a transaction, so callers that need the audit write to commit
// atomically with their business change can run Create on a tx-bound Core.
type Store interface {
	WithTx(tx sqlx.ExtContext) Store
	Save(ctx context.Context, al AuditLog) error
	QueryLastByModelID(ctx context.Context, modelType, modelID string) (AuditLog, error)
	QueryHistoryByModelID(ctx context.Context, modelType, modelID string) ([]AuditLog, error)
	QueryModelByVersion(ctx context.Context, modelType, modelID string, ver int) (AuditLog, error)

	// Versions returns the stored version numbers for a model, ascending. Used by
	// Compact to decide what to thin.
	Versions(ctx context.Context, modelType, modelID string) ([]int, error)
	// DeleteVersions removes the given versions of a model and returns the count
	// deleted. Used by Compact.
	DeleteVersions(ctx context.Context, modelType, modelID string, versions []int) (int, error)
	// OverThreshold lists models whose stored-version count exceeds threshold, up
	// to limit. Used by the background Sweeper to find compaction candidates.
	OverThreshold(ctx context.Context, threshold, limit int) ([]ModelRef, error)
}

// ModelRef identifies a model and how many versions it has stored.
type ModelRef struct {
	ModelType string
	ModelID   string
	Versions  int
}

// NewCore constructs a Core over the given store. log may be nil.
func NewCore(log *logger.Logger, store Store, opts ...Option) *Core {
	c := &Core{log: log, store: store}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// QueryLastByModelID returns the most recent entry for a model, or ErrNotFound.
func (c *Core) QueryLastByModelID(ctx context.Context, modelType, modelID string) (AuditLog, error) {
	last, err := c.store.QueryLastByModelID(ctx, modelType, modelID)
	if err != nil {
		return AuditLog{}, fmt.Errorf("auditlog: query last %s/%s: %w", modelType, modelID, err)
	}
	return last, nil
}

// QueryHistoryByModelID returns every version of a model in ascending order.
func (c *Core) QueryHistoryByModelID(ctx context.Context, modelType, modelID string) ([]AuditLog, error) {
	hist, err := c.store.QueryHistoryByModelID(ctx, modelType, modelID)
	if err != nil {
		return nil, fmt.Errorf("auditlog: query history %s/%s: %w", modelType, modelID, err)
	}
	return hist, nil
}

// QueryDiffByModelID returns a textual diff between two versions of a model.
// Both filter.CurrentVersion and filter.TargetVersion are required.
func (c *Core) QueryDiffByModelID(ctx context.Context, modelType, modelID string, filter QueryFilter) (string, error) {
	if filter.CurrentVersion == nil || filter.TargetVersion == nil {
		return "", fmt.Errorf("auditlog: %w: both current and target versions are required", ErrInvalidFilter)
	}

	cur, err := c.store.QueryModelByVersion(ctx, modelType, modelID, *filter.CurrentVersion)
	if err != nil {
		return "", fmt.Errorf("auditlog: query version %d of %s/%s: %w", *filter.CurrentVersion, modelType, modelID, err)
	}

	target, err := c.store.QueryModelByVersion(ctx, modelType, modelID, *filter.TargetVersion)
	if err != nil {
		return "", fmt.Errorf("auditlog: query version %d of %s/%s: %w", *filter.TargetVersion, modelType, modelID, err)
	}

	diffs, err := diff(cur.Payload, target.Payload)
	if err != nil {
		return "", fmt.Errorf("auditlog: diff %s/%s: %w", modelType, modelID, err)
	}
	return diffs, nil
}

// QueryDiffAllVersionByModelID finds the diffs between all versions of the model
func (c *Core) QueryDiffAllVersionByModelID(ctx context.Context, modelType, modelID string, filter QueryFilter) ([]DiffRecord, error) {
	modelHistory, err := c.QueryHistoryByModelID(ctx, modelType, modelID)
	if err != nil {
		return []DiffRecord{}, fmt.Errorf("query auditlog history by modelID[%s], modelType[%s] %w", modelID, modelType, err)
	}

	if len(modelHistory) < 2 {
		return []DiffRecord{}, nil
	}

	// iterate over all versions of the model and compare to previous version
	var recs []DiffRecord // diff between all versions

	ver1 := DiffRecord{
		ID:        modelHistory[0].ID,
		ModelID:   modelHistory[0].ModelID,
		ModelType: modelHistory[0].ModelType,
		Method:    modelHistory[0].Method,
		Path:      modelHistory[0].Path,
		Version:   modelHistory[0].Version,
		UpdatedBy: &modelHistory[0].CreatedBy,
		UpdatedAt: &modelHistory[0].CreatedAt,
	}

	for i := 1; i < len(modelHistory); i++ {
		rec := DiffRecord{
			ID:        modelHistory[i].ID,
			ModelID:   modelHistory[i].ModelID,
			ModelType: modelHistory[i].ModelType,
			Method:    modelHistory[i].Method,
			Path:      modelHistory[i].Path,
			Version:   modelHistory[i].Version,
			UpdatedBy: &modelHistory[i].CreatedBy,
			UpdatedAt: &modelHistory[i].CreatedAt,
		}
		v1, err := unmarshalJSON(string(modelHistory[i-1].Payload))
		if err != nil {
			return []DiffRecord{}, fmt.Errorf("query auditlog history by modelID[%s], modelType[%s] %w", modelID, modelType, err)
		}

		v2, err := unmarshalJSON(string(modelHistory[i].Payload))
		if err != nil {
			return []DiffRecord{}, fmt.Errorf("query auditlog history by modelID[%s], modelType[%s] %w", modelID, modelType, err)
		}

		d := compareJSON(v1, v2)
		for key, change := range d {
			if i == 1 {
				ver1.ChangedFields = append(ver1.ChangedFields, Field{
					Key:      key,
					NewValue: change["old_value"],
					OldValue: nil,
				})
			}

			field := Field{
				Key:      key,
				NewValue: change["new_value"],
				OldValue: change["old_value"],
			}
			rec.ChangedFields = append(rec.ChangedFields, field)
		}
		if i == 1 {
			recs = append(recs, ver1)
		}
		recs = append(recs, rec)
	}

	return recs, nil
}

// maxCreateAttempts bounds the retry on a concurrent version collision.
const maxCreateAttempts = 3

// Create records a new version of a model. When the new snapshot is identical to
// the latest stored one (ignoring the normalized-away updated_at/updated_by
// fields) no new version is written and a zero AuditLog is returned. The returned
// AuditLog has Version == 0 exactly when nothing was stored.
//
// Create is a read-modify-write (read latest version, insert version+1). If a
// concurrent writer inserts the same version first, the UNIQUE constraint rejects
// this insert; Create then re-reads the latest version and retries, so concurrent
// distinct changes are not lost (up to maxCreateAttempts).
func (c *Core) Create(ctx context.Context, in NewAuditLog) (AuditLog, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(in.Payload); err != nil {
		return AuditLog{}, fmt.Errorf("auditlog: encode payload: %w", err)
	}

	for attempt := 1; ; attempt++ {
		last, err := c.QueryLastByModelID(ctx, in.ModelType, in.ModelID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return AuditLog{}, err
		}

		// If a previous version exists and is unchanged, skip writing a new one.
		if last.Version != 0 {
			unchanged, err := payloadsEqual(body.Bytes(), last.Payload)
			if err != nil {
				return AuditLog{}, err
			}
			if unchanged {
				return AuditLog{}, nil
			}
		}

		entry := AuditLog{
			Version:   last.Version + 1,
			ModelType: in.ModelType,
			ModelID:   in.ModelID,
			Method:    in.Method,
			Path:      in.Path,
			Payload:   body.Bytes(),
			CreatedAt: time.Now().UTC(),
			CreatedBy: in.CreatedBy,
		}
		err = c.store.Save(ctx, entry)
		if err == nil {
			c.maybeAutoCompact(ctx, entry)
			return entry, nil
		}
		// A racing writer took this version; re-read and retry.
		if errors.Is(err, dbx.ErrDBDuplicatedEntry) && attempt < maxCreateAttempts {
			continue
		}
		return AuditLog{}, fmt.Errorf("auditlog: save %s/%s v%d: %w", entry.ModelType, entry.ModelID, entry.Version, err)
	}
}

// maybeAutoCompact opportunistically compacts the model just written, gated by
// version number so the cost is amortized. Best-effort: errors are logged.
func (c *Core) maybeAutoCompact(ctx context.Context, entry AuditLog) {
	if c.autoCompactEvery <= 0 || entry.Version%c.autoCompactEvery != 0 {
		return
	}
	if _, err := c.Compact(ctx, entry.ModelType, entry.ModelID, c.autoCompactOpts); err != nil && c.log != nil {
		c.log.Error(ctx, "auditlog: auto-compact failed",
			"model_type", entry.ModelType, "model_id", entry.ModelID, "err", err)
	}
}

// Record calls Create and logs any error instead of returning it, satisfying
// Recorder. The transport adapters use it so an audit failure never affects the
// response the user already received.
func (c *Core) Record(ctx context.Context, entry NewAuditLog) {
	if _, err := c.Create(ctx, entry); err != nil && c.log != nil {
		c.log.Error(ctx, "auditlog: record failed",
			"model_type", entry.ModelType, "model_id", entry.ModelID, "err", err)
	}
}
