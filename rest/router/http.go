package router

import "net/http"

// Middleware is standard net/http middleware: it wraps an http.Handler. It is
// the transport layer's currency; the application layer uses rest.MidFunc.
type Middleware = func(http.Handler) http.Handler

// Use appends transport (net/http) middleware to this group, applied to every
// route registered on it afterwards, outermost first. It is the transport twin
// of UseApp and shadows the embedded routegroup Use so the whole transport
// surface stays visible on Router in go doc / pkg.go.dev, next to its App twins,
// rather than hidden behind the embed.
//
// Caveat inherited from routegroup: root-level transport middleware runs before
// the request is matched to a route, so r.PathValue is empty inside it. Add the
// middleware to a Mount/With sub-group to run after matching, with path values.
func (r *Router) Use(mws ...Middleware) {
	if len(mws) == 0 {
		return
	}
	r.Bundle.Use(mws[0], mws[1:]...)
}

// With returns a sub-router that additionally runs mws (transport middleware),
// scoped to routes registered on the returned router — the parent is never
// affected. It is the transport twin of WithApp. Use it to keep a concern (a
// request timeout, a body-size limit) off sibling routes such as debug/probes.
func (r *Router) With(mws ...Middleware) *Router {
	if len(mws) == 0 {
		return r
	}
	return r.clone(r.Bundle.With(mws[0], mws[1:]...))
}

// Handle registers a standard net/http handler, optionally wrapping it with
// per-route transport middleware (mws, outermost first). It is the transport
// twin of HandleApp: use Handle for a raw http.Handler (a probe, a file server,
// a proxied or third-party handler), HandleApp for a typed rest.HandlerFunc.
// With no mws it is identical to the embedded routegroup Handle it shadows, so
// existing calls keep working; for middleware shared across several routes
// prefer a sub-group via With over repeating mws per route.
func (r *Router) Handle(pattern string, h http.Handler, mws ...Middleware) {
	if len(mws) > 0 {
		r.Bundle.With(mws[0], mws[1:]...).Handle(pattern, h)
		return
	}
	r.Bundle.Handle(pattern, h)
}

// HandleFunc is Handle's http.HandlerFunc convenience wrapper — the same choice
// as Handle for a handler you already hold as a function value.
func (r *Router) HandleFunc(pattern string, h http.HandlerFunc, mws ...Middleware) {
	r.Handle(pattern, h, mws...)
}
