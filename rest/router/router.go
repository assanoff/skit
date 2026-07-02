package router

import (
	"net/http"

	"github.com/go-pkgz/routegroup"

	"github.com/assanoff/skit/rest"
)

// Router wraps routegroup.Bundle with a typed application layer that sits beside
// the standard net/http transport layer. Every concern comes as a pair: the bare
// verb is transport (Use/With, Handle/HandleFunc), the App-suffixed twin is the
// typed layer (UseApp/WithApp, HandleApp). Transport middleware always wraps the
// boundary (outermost), application middleware runs inside it — see the package
// doc for the model.
//
// The zero value is not usable; construct with New. The two surfaces live in
// sibling files: http.go (net/http) and app.go (typed rest).
type Router struct {
	*routegroup.Bundle
	appMids []rest.MidFunc
}

// New creates a Router backed by a fresh ServeMux. appMids are applied to every
// handler registered via HandleApp (e.g. request-context, error localization),
// outermost first.
func New(appMids ...rest.MidFunc) *Router {
	return &Router{
		Bundle:  routegroup.New(http.NewServeMux()),
		appMids: appMids,
	}
}

// clone returns a Router sharing this one's application middleware but backed by
// the given bundle, so the composition helpers (Mount/Route/With/WithApp) stay
// in the *Router type instead of leaking the embedded *routegroup.Bundle.
func (r *Router) clone(b *routegroup.Bundle) *Router {
	return &Router{Bundle: b, appMids: r.appMids}
}

// Mount returns a sub-router rooted at pattern: routes registered on it are
// prefixed by pattern, and it inherits the current transport and application
// middleware.
func (r *Router) Mount(pattern string) *Router {
	return r.clone(r.Bundle.Mount(pattern))
}

// Route registers a nested group via a callback that receives a sub-router, so
// any middleware the callback adds is scoped to that group and never leaks back
// to the parent.
func (r *Router) Route(fn func(*Router)) {
	r.Bundle.Route(func(b *routegroup.Bundle) {
		fn(r.clone(b))
	})
}
