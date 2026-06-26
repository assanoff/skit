// Package auditrest is the REST integration for auditlog: a read-side handler
// group (history / diff / changes) mountable on any skit router in one
// call. See handlers.go.
//
// Recording is intentionally not done here — audit at the domain layer via the
// auditbus eventbus adapter or a direct auditlog.Core.Create, which covers every
// transport (HTTP, gRPC, workers, consumers) uniformly.
package auditrest
