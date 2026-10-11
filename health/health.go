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
		writeJSON(w, http.StatusOK, report{Status: "ok"})
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
				continue
			}
			results[c.Name] = "ok"
		}

		status := http.StatusOK
		overall := "ok"
		if !healthy {
			status = http.StatusServiceUnavailable
			overall = "unavailable"
		}
		writeJSON(w, status, report{Status: overall, Checks: results})
	}
}

// report is the JSON body of the health endpoints.
type report struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body report) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) //nolint:errchkjson // a failed write means the client is gone
}
