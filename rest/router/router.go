package router

import (
	"net/http"

	"github.com/go-pkgz/routegroup"

	"github.com/assanoff/skit/rest"
)

// Middleware is standard net/http middleware.
type Middleware = func(http.Handler) http.Handler

// Router is a thin wrapper over routegroup.Bundle that additionally knows how to
// mount typed rest.HandlerFunc handlers. The zero value is not usable; use New.
type Router struct {
	*routegroup.Bundle
	appMids []rest.MidFunc
}

// New creates a Router backed by a fresh ServeMux. appMids are applied to every
// handler registered via HandleApp (e.g. error mapping, auth), outermost first.
func New(appMids ...rest.MidFunc) *Router {
	return &Router{
		Bundle:  routegroup.New(http.NewServeMux()),
		appMids: appMids,
	}
}

func (r *Router) clone(b *routegroup.Bundle) *Router {
	return &Router{Bundle: b, appMids: r.appMids}
}

// Mount returns a sub-router rooted at pattern.
func (r *Router) Mount(pattern string) *Router {
	return r.clone(r.Bundle.Mount(pattern))
}

// Route registers nested routes via a callback that receives a sub-router.
func (r *Router) Route(fn func(*Router)) {
	r.Bundle.Route(func(b *routegroup.Bundle) {
		fn(r.clone(b))
	})
}

// With returns a sub-router with the given net/http (transport) middleware
// applied. Pair concept: WithApp adds typed (application) middleware.
func (r *Router) With(mws ...Middleware) *Router {
	if len(mws) == 0 {
		return r
	}
	return r.clone(r.Bundle.With(mws[0], mws[1:]...))
}

// UseApp appends application middleware (rest.MidFunc) to this group: it wraps
// every typed handler registered here and on sub-groups created afterwards. It
// is the typed-layer counterpart of the embedded Use (which adds net/http
// transport middleware). Existing app middleware stays outermost, so global
// concerns (request-context, error localization) keep wrapping the ones added
// here (e.g. group auth). Copy-on-write: groups already derived are unaffected.
func (r *Router) UseApp(mw ...rest.MidFunc) {
	if len(mw) == 0 {
		return
	}
	r.appMids = appendMids(r.appMids, mw)
}

// WithApp returns a sub-router whose typed handlers additionally run mw, without
// starting a new transport group (it shares this router's middleware bundle).
// It is the typed-layer counterpart of With. Use it to scope app middleware to a
// group of routes, e.g. admin := api.WithApp(auth).
func (r *Router) WithApp(mw ...rest.MidFunc) *Router {
	if len(mw) == 0 {
		return r
	}
	return &Router{Bundle: r.Bundle, appMids: appendMids(r.appMids, mw)}
}

// HandleApp registers a typed rest.HandlerFunc on this group. The router's
// appMids (outermost) plus any per-route mids are applied, and the chained
// handler is registered as an http.Handler via the embedded bundle — so the
// group's transport middleware (Use/With) wraps it, and rest.Respond encodes the
// returned ResponseEncoder. Its method value is a rest.Handle, so an Install function
// passes r.HandleApp to a feature's Routes method as the registration seam.
func (r *Router) HandleApp(pattern string, h rest.HandlerFunc, mids ...rest.MidFunc) {
	// appMids run outermost, then per-route mids, then the handler. The chained
	// value is a rest.HandlerFunc, which is itself an http.Handler (ServeHTTP).
	r.Handle(pattern, rest.ChainMiddleware(h, appendMids(r.appMids, mids)...))
}

// appendMids returns base followed by extra as a fresh slice, so callers never
// append in place into a slice shared by other Router values.
func appendMids(base, extra []rest.MidFunc) []rest.MidFunc {
	out := make([]rest.MidFunc, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return out
}
