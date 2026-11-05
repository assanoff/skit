// Package pgschema provisions the Postgres tables of the SDK storage packages
// at startup (queue, auditlog/auditpg, translation/translationpg, settings)
// and derives the advisory-lock keys of the SDK (lock, partition).
//
// Ensure runs the idempotent DDL of a package in a transaction under an
// advisory lock keyed by "skit.<pkg>.schema", so the replicas booting at once
// serialize instead of racing on CREATE TABLE:
//
//	err := pgschema.Ensure(ctx, log, db, "auditlog", Schema()) // db is a *dbx.DB
//
// Key hashes a name into an advisory-lock key with FNV-1a. The hash never
// changes: the replicas of two versions of a service must agree on the key.
package pgschema
