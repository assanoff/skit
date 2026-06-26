package router_test

import (
	"context"
	"net/http"
	"time"

	"github.com/assanoff/skit/middleware"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/router"
)

// Example shows the two-layer assembly: global transport and application
// middleware, a business sub-group carrying request guards, per-route and
// per-group application middleware, and a standard handler living next to the
// typed ones on the same router.
func Example() {
	// A typed handler returns a ResponseEncoder; errors are returned, not written.
	widget := func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON(map[string]string{"name": "gadget"})
	}

	// An application middleware (rest.MidFunc) — here a trivial guard.
	requireAuth := func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			if r.Header.Get("Authorization") == "" {
				return rest.JSONStatus(map[string]string{"error": "unauthorized"}, http.StatusUnauthorized)
			}
			return next(ctx, r)
		}
	}

	r := router.New( /* global application middleware, e.g. reqctx, localize-errors */ )

	// Transport middleware for every route, including debug: panic-first.
	r.Use(
		middleware.Panics(nil),
		middleware.AccessLog(nil),
	)

	// Probes stay outside the request guards below.
	r.HandleApp("GET /healthz", func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON(map[string]string{"status": "ok"})
	})

	// Business sub-group: request timeout + body-size limit (transport), not on
	// the probe above.
	api := r.With(
		middleware.Timeout(5*time.Second),
		middleware.SizeLimit(1<<20),
	)
	api.HandleApp("GET /widgets", widget) // public read

	// A whole group behind auth via WithApp (application sub-group).
	admin := api.WithApp(requireAuth)
	admin.HandleApp("POST /widgets", widget)

	// A standard http.Handler on the same router (transport stack only).
	r.Handle("GET /assets/", http.FileServer(http.Dir("./assets")))

	// r is an http.Handler: http.ListenAndServe(":8080", r)
	_ = r
}
