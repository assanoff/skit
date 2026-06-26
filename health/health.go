package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Checker reports the health of a dependency. A nil error means healthy.
type Checker func(ctx context.Context) error

// NamedChecker pairs a Checker with a name for per-dependency reporting.
type NamedChecker struct {
	Name  string
	Check Checker
}

// Liveness returns a handler that always reports 200 OK. Use it for the liveness
// probe so a slow dependency does not cause pod restarts.
func Liveness() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// Readiness returns a handler that runs all checks and reports 200 when every
// check passes, or 503 with per-check details otherwise. checkTimeout bounds the
// whole set of checks (0 = no bound).
func Readiness(checkTimeout time.Duration, checks ...NamedChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if checkTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, checkTimeout)
			defer cancel()
		}

		results := make(map[string]string, len(checks))
		healthy := true
		for _, c := range checks {
			if err := c.Check(ctx); err != nil {
				healthy = false
				results[c.Name] = err.Error()
			} else {
				results[c.Name] = "ok"
			}
		}

		status := http.StatusOK
		overall := "ok"
		if !healthy {
			status = http.StatusServiceUnavailable
			overall = "unavailable"
		}
		writeJSON(w, status, map[string]any{"status": overall, "checks": results})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
