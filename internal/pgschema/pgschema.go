// Package pgschema provisions the Postgres tables of the SDK storage packages
// at startup and derives the advisory-lock keys of the SDK.
package pgschema

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"

	"github.com/assanoff/skit/dbx"
)

// Key derives a stable Postgres advisory-lock key from name using FNV-1a, so a
// package claims a collision-resistant key without hand-picking a magic
// integer that might clash with the application's own advisory locks —
// advisory locks share one namespace per database. The hash never changes:
// replicas of two versions of a service must agree on the key of a name.
func Key(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name)) // hash.Hash.Write never returns an error
	return int64(h.Sum64())      //nolint:gosec // G115: intentional reinterpret of a 64-bit hash to a stable advisory key
}

// Ensure applies the idempotent ddl of the package pkg once, safely, at
// service startup. It runs ddl inside a transaction holding a Postgres
// transaction-scoped advisory lock keyed by "skit.<pkg>.schema", so when
// several replicas boot together they serialize on the lock and concurrent
// CREATE ... IF NOT EXISTS statements do not race ("tuple concurrently
// updated" / duplicate-relation errors). The lock releases when the
// transaction ends — commit, rollback, or a dropped connection — so it cannot
// leak. ddl runs as written, without :name parsing.
func Ensure(ctx context.Context, log *slog.Logger, db *dbx.DB, pkg, ddl string) error {
	key := Key("skit." + pkg + ".schema")
	return dbx.WithinTran(ctx, log, db, func(tx *dbx.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", key); err != nil {
			return fmt.Errorf("pgschema: acquire the schema lock of %s: %w", pkg, err)
		}
		return dbx.Exec(ctx, tx, ddl)
	})
}
