// Package db is the Postgres implementation of auditlog.Store. It maps between
// the domain AuditLog and its row representation and uses the skit dbx
// helpers for query logging and error translation. Inline const queries run
// against the fixed audit_log table.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/auditlog"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/logger"
)

// Store implements auditlog.Store against Postgres.
type Store struct {
	log *logger.Logger
	db  sqlx.ExtContext
}

// NewStore builds a Store. Pass a *sqlx.DB for pool-backed use.
func NewStore(log *logger.Logger, db *sqlx.DB) *Store {
	return &Store{log: log, db: db}
}

// Compile-time check that Store satisfies the domain contract.
var _ auditlog.Store = (*Store)(nil)

// WithTx returns a sibling Store whose queries run on tx, so an audit write can
// commit atomically with the caller's business change.
func (s *Store) WithTx(tx sqlx.ExtContext) auditlog.Store {
	return &Store{log: s.log, db: tx}
}

// schemaLockKey is a fixed application-defined key for the transaction-level
// advisory lock that serializes EnsureSchema across processes.
const schemaLockKey = 0x6175_6469_746c_6f67 // "auditlog" as hex

// EnsureSchema creates the audit_log table and indexes if absent.
//
// Prefer running the DDL as a migration in production — goose already serializes
// migrations with its own advisory lock. EnsureSchema is for tests and simple
// boot-time setup; when several replicas call it concurrently, a bare
// CREATE TABLE IF NOT EXISTS can still race ("tuple concurrently updated"), so
// the DDL runs under a transaction-level advisory lock that auto-releases on
// commit. A tx-bound store (via WithTx) just runs the DDL — the caller owns the
// transaction and any locking.
func (s *Store) EnsureSchema(ctx context.Context) error {
	db, ok := s.db.(*sqlx.DB)
	if !ok {
		return dbx.ExecContext(ctx, s.log, s.db, Schema())
	}
	return dbx.WithinTran(ctx, s.log, db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", int64(schemaLockKey)); err != nil {
			return fmt.Errorf("auditlog: advisory lock: %w", err)
		}
		return dbx.ExecContext(ctx, s.log, tx, Schema())
	})
}

// Save inserts a new audit log entry.
func (s *Store) Save(ctx context.Context, al auditlog.AuditLog) error {
	const q = `
		INSERT INTO audit_log
			(model_id, model_type, version, method, path, payload, created_at, created_by)
		VALUES
			(:model_id, :model_type, :version, :method, :path, :payload, :created_at, :created_by)`
	if err := dbx.NamedExecContext(ctx, s.log, s.db, q, toDBAuditLog(al)); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

// QueryLastByModelID returns the most recent entry for a model.
func (s *Store) QueryLastByModelID(ctx context.Context, modelType, modelID string) (auditlog.AuditLog, error) {
	data := struct {
		ModelID   string `db:"model_id"`
		ModelType string `db:"model_type"`
	}{ModelID: modelID, ModelType: modelType}

	const q = `
		SELECT id, model_id, version, method, path, model_type, payload, created_at, created_by
		FROM audit_log
		WHERE model_id = :model_id AND model_type = :model_type
		ORDER BY version DESC
		LIMIT 1`

	var row auditLogDB
	if err := dbx.NamedQueryStruct(ctx, s.log, s.db, q, data, &row); err != nil {
		if errors.Is(err, dbx.ErrDBNotFound) {
			return auditlog.AuditLog{}, fmt.Errorf("querylastbymodelid: %w", auditlog.ErrNotFound)
		}
		return auditlog.AuditLog{}, fmt.Errorf("querylastbymodelid: %w", err)
	}
	return toCoreAuditLog(row), nil
}

// QueryHistoryByModelID returns every version of a model in ascending order.
func (s *Store) QueryHistoryByModelID(ctx context.Context, modelType, modelID string) ([]auditlog.AuditLog, error) {
	data := struct {
		ModelType string `db:"model_type"`
		ModelID   string `db:"model_id"`
	}{ModelType: modelType, ModelID: modelID}

	const q = `
		SELECT id, model_id, version, method, path, model_type, payload, created_at, created_by
		FROM audit_log
		WHERE model_id = :model_id AND model_type = :model_type
		ORDER BY version ASC`

	var rows []auditLogDB
	if err := dbx.NamedQuerySlice(ctx, s.log, s.db, q, data, &rows); err != nil {
		return nil, fmt.Errorf("queryhistorybymodelid: %w", err)
	}
	return toCoreAuditLogSlice(rows), nil
}

// QueryModelByVersion returns a single version of a model.
func (s *Store) QueryModelByVersion(ctx context.Context, modelType, modelID string, ver int) (auditlog.AuditLog, error) {
	data := struct {
		ModelType string `db:"model_type"`
		ModelID   string `db:"model_id"`
		Version   int    `db:"version"`
	}{ModelType: modelType, ModelID: modelID, Version: ver}

	const q = `
		SELECT id, model_id, version, method, path, model_type, payload, created_at, created_by
		FROM audit_log
		WHERE model_id = :model_id AND model_type = :model_type AND version = :version`

	var row auditLogDB
	if err := dbx.NamedQueryStruct(ctx, s.log, s.db, q, data, &row); err != nil {
		if errors.Is(err, dbx.ErrDBNotFound) {
			return auditlog.AuditLog{}, fmt.Errorf("querymodelbyversion: %w", auditlog.ErrNotFound)
		}
		return auditlog.AuditLog{}, fmt.Errorf("querymodelbyversion: %w", err)
	}
	return toCoreAuditLog(row), nil
}

// Versions returns the stored version numbers for a model, ascending.
func (s *Store) Versions(ctx context.Context, modelType, modelID string) ([]int, error) {
	const q = `SELECT version FROM audit_log WHERE model_type = $1 AND model_id = $2 ORDER BY version ASC`
	var vers []int
	if err := sqlx.SelectContext(ctx, s.db, &vers, q, modelType, modelID); err != nil {
		return nil, fmt.Errorf("versions: %w", err)
	}
	return vers, nil
}

// DeleteVersions removes the given versions of a model and returns the count
// deleted.
func (s *Store) DeleteVersions(ctx context.Context, modelType, modelID string, versions []int) (int, error) {
	if len(versions) == 0 {
		return 0, nil
	}
	q, args, err := sqlx.In(`DELETE FROM audit_log WHERE model_type = ? AND model_id = ? AND version IN (?)`,
		modelType, modelID, versions)
	if err != nil {
		return 0, fmt.Errorf("deleteversions: build: %w", err)
	}
	res, err := s.db.ExecContext(ctx, s.db.Rebind(q), args...)
	if err != nil {
		return 0, fmt.Errorf("deleteversions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("deleteversions: rows affected: %w", err)
	}
	return int(n), nil
}

// OverThreshold lists models whose stored-version count exceeds threshold,
// most-versioned first, up to limit.
func (s *Store) OverThreshold(ctx context.Context, threshold, limit int) ([]auditlog.ModelRef, error) {
	const q = `
		SELECT model_type, model_id, count(*) AS versions
		FROM audit_log
		GROUP BY model_type, model_id
		HAVING count(*) > $1
		ORDER BY versions DESC
		LIMIT $2`
	var rows []struct {
		ModelType string `db:"model_type"`
		ModelID   string `db:"model_id"`
		Versions  int    `db:"versions"`
	}
	if err := sqlx.SelectContext(ctx, s.db, &rows, q, threshold, limit); err != nil {
		return nil, fmt.Errorf("overthreshold: %w", err)
	}
	out := make([]auditlog.ModelRef, len(rows))
	for i, r := range rows {
		out[i] = auditlog.ModelRef{ModelType: r.ModelType, ModelID: r.ModelID, Versions: r.Versions}
	}
	return out, nil
}

// Schema returns the DDL creating the audit_log table and its indexes. Apply it
// in a migration, or call Store.EnsureSchema in tests. The UNIQUE constraint on
// (model_type, model_id, version) makes the read-modify-write in Core.Create safe
// under concurrency: a racing insert at the same version fails instead of
// duplicating.
func Schema() string {
	return `
CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    model_type  TEXT NOT NULL,
    model_id    TEXT NOT NULL,
    version     INT NOT NULL,
    method      TEXT NOT NULL DEFAULT '',
    path        TEXT NOT NULL DEFAULT '',
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT NOT NULL DEFAULT '',
    UNIQUE (model_type, model_id, version)
);
CREATE INDEX IF NOT EXISTS audit_log_model_idx ON audit_log (model_type, model_id, version);`
}
