// Package health provides liveness and readiness HTTP handlers suitable for
// Kubernetes probes.
//
// Liveness reflects "the process is up" and always returns 200, so a slow
// dependency does not cause pod restarts. Readiness reflects "dependencies (DB,
// broker, ...) are reachable" by running pluggable checks and returning 200 when
// all pass or 503 with per-check details otherwise. Both handlers respond with a
// small JSON body.
//
// # Usage
//
// Wrap each dependency probe as a NamedChecker and pass them to Readiness;
// register the handlers on your router (or hand them to debugsrv):
//
//	dbCheck := health.NamedChecker{
//		Name:  "postgres",
//		Check: func(ctx context.Context) error { return db.PingContext(ctx) },
//	}
//
//	mux.Handle("/healthz", health.Liveness())
//	mux.Handle("/readyz", health.Readiness(2*time.Second, dbCheck))
//
// # API
//
//   - Checker: func(ctx) error reporting a dependency's health; nil = healthy.
//   - NamedChecker: a Checker paired with a Name for per-dependency reporting.
//   - Liveness(): handler that always reports 200 OK.
//   - Readiness(checkTimeout, checks...): runs every check and reports 200 or
//     503. checkTimeout bounds the whole set of checks (0 = no bound).
package health
