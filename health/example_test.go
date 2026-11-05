package health_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/assanoff/skit/health"
)

// Readiness runs every check: 200 when all pass, 503 with the failed ones
// otherwise. Liveness is always 200, so a slow dependency restarts no pod.
func Example() {
	db := health.NamedChecker{
		Name:  "postgres",
		Check: func(context.Context) error { return nil },
	}
	broker := health.NamedChecker{
		Name: "rabbitmq",
		Check: func(context.Context) error {
			return errors.New("connection refused")
		},
	}
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Liveness())
	mux.Handle("GET /readyz", health.Readiness(2*time.Second, db, broker))

	for _, path := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		fmt.Println(w.Code, w.Body.String())
	}
	// Output:
	// 200 {"status":"ok"}
	// 503 {"status":"unavailable","checks":{"postgres":"ok","rabbitmq":"connection refused"}}
}
