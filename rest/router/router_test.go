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

// recorder collects an ordered trace of which middleware ran, so tests can
// assert the transport-outside / application-inside nesting.
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

func (rec *recorder) handler(name string) rest.HandlerFunc {
	return func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		rec.order = append(rec.order, name)
		return rest.JSON("ok")
	}
}

func get(r *router.Router, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestNestingOrder locks the model: transport middleware wraps the boundary
// (outermost), then global app middleware, then group app middleware (WithApp),
// then per-route middleware, then the handler.
func TestNestingOrder(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global")) // global application middleware
	r.Use(rec.transportMid("transport"))      // global transport middleware

	admin := r.WithApp(rec.appMid("app-group")) // application sub-group
	admin.HandleApp("GET /x", rec.handler("handler"), rec.appMid("app-route"))

	is.Equal(get(r, "/x").Code, http.StatusOK) // route serves 200
	is.Equal(rec.order, []string{"transport", "app-global", "app-group", "app-route", "handler"})
}

// TestStandardHandlerSkipsAppMiddleware verifies a raw http.Handler registered
// via the embedded Handle runs transport middleware but NOT application
// middleware (app middleware only wraps HandleApp routes).
func TestStandardHandlerSkipsAppMiddleware(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global"))
	r.Use(rec.transportMid("transport"))

	r.Handle("GET /raw", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rec.order = append(rec.order, "raw")
		w.WriteHeader(http.StatusTeapot)
	}))

	is.Equal(get(r, "/raw").Code, http.StatusTeapot)
	// app middleware must not wrap a raw handler — only the transport stack does.
	is.Equal(rec.order, []string{"transport", "raw"})
}

// TestWithAppDoesNotLeak verifies WithApp is immutable: app middleware added to
// a derived group does not affect routes registered on the parent.
func TestWithAppDoesNotLeak(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global"))
	admin := r.WithApp(rec.appMid("app-admin"))
	admin.HandleApp("GET /admin", rec.handler("admin-handler"))
	r.HandleApp("GET /public", rec.handler("public-handler")) // parent, no app-admin

	get(r, "/public")
	is.Equal(rec.order, []string{"app-global", "public-handler"}) // parent unaffected

	rec.order = nil
	get(r, "/admin")
	is.Equal(rec.order, []string{"app-global", "app-admin", "admin-handler"})
}

// TestUseApp verifies the mutating UseApp applies to handlers registered after
// it, with existing app middleware staying outermost.
func TestUseApp(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global"))
	r.UseApp(rec.appMid("app-added"))
	r.HandleApp("GET /y", rec.handler("handler"))

	get(r, "/y")
	is.Equal(rec.order, []string{"app-global", "app-added", "handler"})
}
