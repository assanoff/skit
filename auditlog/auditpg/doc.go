// Package auditpg is the Postgres auditlog.Store: the versions of the models
// in the audit_log table, one row per version, the snapshot of the model in a
// JSONB column. Queries are inline constants run through the dbx helpers,
// which log them and translate the errors of the driver.
//
// # Usage
//
// Build the store over the pool and provision its table at startup;
// EnsureSchema is safe on several replicas booting at once:
//
//	store := auditpg.NewStore(log, db) // db is a *dbx.DB
//	if err := store.EnsureSchema(ctx); err != nil {
//	    return err
//	}
//	core := auditlog.NewCore(log, store)
//
// A store over a transaction (*dbx.Tx) commits the audit entry with the
// business change of the transaction:
//
//	err := dbx.WithinTran(ctx, log, db, func(tx *dbx.Tx) error {
//	    // ... the business write on tx
//	    _, err := auditlog.NewCore(log, auditpg.NewStore(log, tx)).Create(ctx, entry)
//	    return err
//	})
//
// Schema returns the DDL for a service that keeps the table in its own
// migrations.
//
// # Table
//
// UNIQUE (model_type, model_id, version) makes the read-modify-write of
// Core.Create safe under concurrency: of two writes at the same version, the
// second fails on the constraint, and Create retries it at the next version.
package auditpg
