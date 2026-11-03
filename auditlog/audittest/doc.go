// Package audittest provides an in-memory auditlog.Store for tests and
// examples, so an audit Core runs without Postgres.
//
// It follows the semantics of the Postgres store: IDs grow from 1, a second
// write of one version of a model fails with dbx.ErrDBDuplicatedEntry, so
// Core.Create retries it at the next version, and a model or a version that
// is not stored is auditlog.ErrNotFound.
//
// # Usage
//
//	core := auditlog.NewCore(log, audittest.NewStore())
//
// # API
//
//   - Store / NewStore: an auditlog.Store over a map, safe for concurrent use.
package audittest
