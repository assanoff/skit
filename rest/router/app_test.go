package router_test

import (
	"net/http"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/rest/router"
)

// TestUseApp verifies the mutating UseApp applies to typed handlers registered
// after it, with existing app middleware staying outermost.
func TestUseApp(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global"))
	r.UseApp(rec.appMid("app-added"))
	r.HandleApp("GET /y", rec.appHandler("handler"))

	is.Equal(get(r, "/y").Code, http.StatusOK)
	is.Equal(rec.order, []string{"app-global", "app-added", "handler"})
}

// TestWithAppDoesNotLeak verifies WithApp is immutable: app middleware added to a
// derived group does not affect routes registered on the parent.
func TestWithAppDoesNotLeak(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global"))
	admin := r.WithApp(rec.appMid("app-admin"))
	admin.HandleApp("GET /admin", rec.appHandler("admin-handler"))
	r.HandleApp("GET /public", rec.appHandler("public-handler")) // parent, no app-admin

	is.Equal(get(r, "/public").Code, http.StatusOK)
	is.Equal(rec.order, []string{"app-global", "public-handler"}) // parent unaffected

	rec.order = nil
	is.Equal(get(r, "/admin").Code, http.StatusOK)
	is.Equal(rec.order, []string{"app-global", "app-admin", "admin-handler"})
}

// TestHandleAppPerRouteMids verifies per-route rest.MidFunc run inside the
// router's global app middleware but outside the handler.
func TestHandleAppPerRouteMids(t *testing.T) {
	is := is.New(t)
	var rec recorder

	r := router.New(rec.appMid("app-global"))
	r.HandleApp("GET /z", rec.appHandler("handler"), rec.appMid("route-a"), rec.appMid("route-b"))

	is.Equal(get(r, "/z").Code, http.StatusOK)
	is.Equal(rec.order, []string{"app-global", "route-a", "route-b", "handler"})
}
