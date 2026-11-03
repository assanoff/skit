package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing/fstest"
	"time"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/middleware"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/router"
)

// Example shows the two-layer assembly: global transport middleware, a
// business sub-group carrying request guards, an application sub-group behind
// auth, and a standard handler living next to the typed ones on the same
// router.
func Example() {
	// A typed handler returns a ResponseEncoder; errors are returned, not written.
	widget := func(_ context.Context, r *http.Request) rest.ResponseEncoder {
		return rest.JSON(map[string]string{"id": rest.Param(r, "id")})
	}
	created := func(context.Context, *http.Request) rest.ResponseEncoder {
		return rest.JSONStatus(map[string]string{"id": "43"}, http.StatusCreated)
	}

	// An application middleware (rest.MidFunc) — here a trivial guard.
	requireAuth := func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			if r.Header.Get("Authorization") == "" {
				return errs.Newf(errs.Unauthenticated, "missing token")
			}
			return next(ctx, r)
		}
	}

	r := router.New( /* global application middleware, e.g. mid.Chain(cfg)... */ )

	// Transport middleware for every route, including debug: panic-first.
	r.Use(middleware.Panics(nil))

	// Probes stay outside the request guards below.
	r.HandleApp("GET /healthz", func(context.Context, *http.Request) rest.ResponseEncoder {
		return rest.JSON(map[string]string{"status": "ok"})
	})

	// Business sub-group: request timeout + body-size limit (transport), not on
	// the probe above.
	api := r.With(
		middleware.Timeout(5*time.Second),
		middleware.SizeLimit(1<<20),
	)
	api.HandleApp("GET /widgets/{id}", widget) // public read

	// A whole group behind auth via WithApp (application sub-group).
	admin := api.WithApp(requireAuth)
	admin.HandleApp("POST /widgets", created)

	// A standard http.Handler on the same router (transport stack only).
	assets := fstest.MapFS{"assets/logo.txt": {Data: []byte("logo")}}
	r.Handle("GET /assets/", http.FileServer(http.FS(assets)))

	// r is an http.Handler: http.ListenAndServe(":8080", r)
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/healthz", nil),
		httptest.NewRequest(http.MethodGet, "/widgets/42", nil),
		httptest.NewRequest(http.MethodPost, "/widgets", nil),
		withToken(httptest.NewRequest(http.MethodPost, "/widgets", nil)),
		httptest.NewRequest(http.MethodGet, "/assets/logo.txt", nil),
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		fmt.Println(req.Method, req.URL.Path, w.Code, strings.TrimSpace(w.Body.String()))
	}
	// Output:
	// GET /healthz 200 {"status":"ok"}
	// GET /widgets/42 200 {"id":"42"}
	// POST /widgets 401 {"code":"unauthenticated","detail":"missing token"}
	// POST /widgets 201 {"id":"43"}
	// GET /assets/logo.txt 200 logo
}

// withToken sets a bearer token on req.
func withToken(req *http.Request) *http.Request {
	req.Header.Set("Authorization", "Bearer t-1")
	return req
}
