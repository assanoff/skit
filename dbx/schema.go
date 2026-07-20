package dbx

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/jmoiron/sqlx"

	"github.com/assanoff/skit/logger"
)

// AdvisoryKey derives a stable Postgres advisory-lock key from name using
// FNV-1a. It lets a package claim a collision-resistant lock key without
// hand-picking a magic integer that might clash with the application's own
// advisory locks — advisory locks share one namespace per database. Use a
// namespaced name such as "skit.outbox.schema".
func AdvisoryKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name)) // hash.Hash.Write never returns an error
	return int64(h.Sum64())      //nolint:gosec // G115: intentional reinterpret of a 64-bit hash to a stable advisory key
}

// EnsureSchema applies idempotent DDL once, safely, at service startup. It runs
// ddl inside a transaction holding a Postgres transaction-scoped advisory lock
// keyed by lockKey, so when several replicas boot together they serialize on the
// lock and concurrent CREATE ... IF NOT EXISTS statements do not race ("tuple
// concurrently updated" / duplicate-relation errors). The lock releases
// automatically when the transaction ends — commit, rollback, or a dropped
// connection — so it cannot leak.
//
// This is how a service auto-provisions the tables an SDK storage package owns
// (outbox, queue, auditlog, translation, ...) at startup, without hand-writing a
// migration for them. lockKey must be unique per schema across the database;
// derive it with AdvisoryKey.
func EnsureSchema(ctx context.Context, log *logger.Logger, db *sqlx.DB, lockKey int64, ddl string) error {
	return WithinTran(ctx, log, db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
			return fmt.Errorf("dbx: acquire schema advisory lock %d: %w", lockKey, err)
		}
		return ExecContext(ctx, log, tx, ddl)
	})
}
