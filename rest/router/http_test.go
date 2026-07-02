package router_test

import (
	"net/http"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/rest/router"
)

// TestUseAppliesGlobally verifies the explicit Use accepts several transport
// middlewares in one call and applies them (outermost first) to routes
// registered afterwards.
func TestUseAppliesGlobally(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New()
	r.Use(rec.transportMid("a"), rec.transportMid("b"))
	r.HandleFunc("GET /x", rec.rawHandler("h"))

	is.Equal(get(r, "/x").Code, http.StatusOK)
	is.Equal(rec.order, []string{"a", "b", "h"})
}

// TestWithScopesAndDoesNotLeak verifies With derives a sub-router whose transport
// middleware wraps only its routes, leaving the parent untouched (immutable).
func TestWithScopesAndDoesNotLeak(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New()
	scoped := r.With(rec.transportMid("scoped"))
	scoped.HandleFunc("GET /scoped", rec.rawHandler("scoped-h"))
	r.HandleFunc("GET /plain", rec.rawHandler("plain-h")) // parent, no scoped mid

	is.Equal(get(r, "/scoped").Code, http.StatusOK)
	is.Equal(rec.order, []string{"scoped", "scoped-h"})

	rec.order = nil
	is.Equal(get(r, "/plain").Code, http.StatusOK)
	is.Equal(rec.order, []string{"plain-h"}) // scoped middleware did not leak to parent
}

// TestHandleSkipsAppMiddleware verifies a raw http.Handler registered via Handle
// runs the transport stack but NOT the application middleware (app middleware
// only wraps HandleApp routes).
func TestHandleSkipsAppMiddleware(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global"))
	r.Use(rec.transportMid("transport"))

	r.Handle("GET /raw", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rec.order = append(rec.order, "raw")
		w.WriteHeader(http.StatusTeapot)
	}))

	is.Equal(get(r, "/raw").Code, http.StatusTeapot)
	is.Equal(rec.order, []string{"transport", "raw"}) // app middleware must not wrap a raw handler
}

// TestHandlePerRouteMiddleware verifies Handle applies per-route transport
// middleware only to its own route, inside the global transport stack, without
// pulling in the application layer or leaking onto a sibling route.
func TestHandlePerRouteMiddleware(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global")) // must NOT wrap a raw Handle route
	r.Use(rec.transportMid("transport"))      // global transport, outermost

	r.Handle("GET /raw", rec.rawHandler("raw"), rec.transportMid("route-mid"))
	r.HandleFunc("GET /plain", rec.rawHandler("plain")) // sibling, no per-route mid

	is.Equal(get(r, "/raw").Code, http.StatusOK)
	is.Equal(rec.order, []string{"transport", "route-mid", "raw"})

	rec.order = nil
	is.Equal(get(r, "/plain").Code, http.StatusOK)
	is.Equal(rec.order, []string{"transport", "plain"}) // no route-mid leak
}
