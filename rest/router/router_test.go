package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/router"
)

// recorder collects an ordered trace of which middleware/handler ran, so tests
// can assert the transport-outside / application-inside nesting. It is shared by
// the router, transport, and app test files (all in package router_test).
type recorder struct {
	order []string
}

// appMid is an application (rest.MidFunc) marker.
func (rec *recorder) appMid(name string) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			rec.order = append(rec.order, name)
			return next(ctx, r)
		}
	}
}

// transportMid is a net/http (func(http.Handler) http.Handler) marker.
func (rec *recorder) transportMid(name string) router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.order = append(rec.order, name)
			next.ServeHTTP(w, r)
		})
	}
}

// appHandler is a typed rest.HandlerFunc marker.
func (rec *recorder) appHandler(name string) rest.HandlerFunc {
	return func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		rec.order = append(rec.order, name)
		return rest.JSON("ok")
	}
}

// rawHandler is a standard http.Handler marker that records and 200s.
func (rec *recorder) rawHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		rec.order = append(rec.order, name)
		w.WriteHeader(http.StatusOK)
	}
}

func get(r *router.Router, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestNestingOrder locks the cross-layer model: transport middleware wraps the
// boundary (outermost), then global app middleware (New), then group app
// middleware (WithApp), then per-route middleware, then the handler.
func TestNestingOrder(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global")) // global application middleware
	r.Use(rec.transportMid("transport"))      // global transport middleware

	admin := r.WithApp(rec.appMid("app-group")) // application sub-group
	admin.HandleApp("GET /x", rec.appHandler("handler"), rec.appMid("app-route"))

	is.Equal(get(r, "/x").Code, http.StatusOK) // route serves 200
	is.Equal(rec.order, []string{"transport", "app-global", "app-group", "app-route", "handler"})
}

// TestMountPrefixesAndInherits verifies Mount roots routes under a path prefix
// and carries the parent's application middleware into the sub-router.
func TestMountPrefixesAndInherits(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global"))
	api := r.Mount("/api")
	api.HandleApp("GET /widgets", rec.appHandler("widgets"))

	is.Equal(get(r, "/api/widgets").Code, http.StatusOK)   // prefixed path resolves
	is.Equal(rec.order, []string{"app-global", "widgets"}) // inherited app middleware ran
	is.Equal(get(r, "/widgets").Code, http.StatusNotFound) // unprefixed path is not registered
}

// TestRouteScopesMiddleware verifies middleware added inside a Route callback is
// scoped to that group and does not leak onto routes registered on the parent.
func TestRouteScopesMiddleware(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New()
	r.Route(func(g *router.Router) {
		g.Use(rec.transportMid("scoped"))
		g.HandleFunc("GET /in", rec.rawHandler("in"))
	})
	r.HandleFunc("GET /out", rec.rawHandler("out")) // sibling on the parent

	is.Equal(get(r, "/in").Code, http.StatusOK)
	is.Equal(rec.order, []string{"scoped", "in"})

	rec.order = nil
	is.Equal(get(r, "/out").Code, http.StatusOK)
	is.Equal(rec.order, []string{"out"}) // scoped middleware did not leak to parent
}
