package router

import "github.com/assanoff/skit/rest"

// UseApp appends application middleware (rest.MidFunc) to this group: it wraps
// every typed handler registered here and on sub-groups created afterwards. It
// is the typed twin of Use. Existing app middleware stays outermost, so global
// concerns (request-context, error localization) keep wrapping ones added here
// (e.g. group auth). Copy-on-write: groups already derived are unaffected.
func (r *Router) UseApp(mw ...rest.MidFunc) {
	if len(mw) == 0 {
		return
	}
	r.appMids = appendMids(r.appMids, mw)
}

// WithApp returns a sub-router whose typed handlers additionally run mw, without
// starting a new transport group (it shares this router's middleware bundle). It
// is the typed twin of With. Use it to scope app middleware to a group of
// routes, e.g. admin := api.WithApp(auth).
func (r *Router) WithApp(mw ...rest.MidFunc) *Router {
	if len(mw) == 0 {
		return r
	}
	return &Router{Bundle: r.Bundle, appMids: appendMids(r.appMids, mw)}
}

// HandleApp registers a typed rest.HandlerFunc on this group. The router's
// appMids (outermost) plus any per-route mids wrap the handler, then the chained
// value registers as an http.Handler via Handle — so the group's transport
// middleware (Use/With) wraps it, and rest.Respond encodes the returned
// ResponseEncoder. Its method value is a rest.Handle, so an Install function can
// pass r.HandleApp to a feature's Routes method as the registration seam.
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
