// Package router wraps the standard library's net/http.ServeMux with nestable
// groups and two cooperating handler/middleware layers: net/http transport
// middleware and typed rest application middleware.
//
// # Two layers, one boundary
//
// A skit service composes handlers at two layers. Pick the layer by what
// the code needs to see — raw bytes and the ResponseWriter, or the typed value a
// handler returns.
//
//	aspect          transport layer (net/http)      application layer (rest)
//	------          --------------------------      ----------------------------
//	handler         http.Handler                    rest.HandlerFunc -> ResponseEncoder
//	middleware      func(http.Handler) http.Handler rest.MidFunc
//	sees            (ResponseWriter, *Request)      (ctx, *Request) -> ResponseEncoder
//	output          bytes on the wire               a typed ResponseEncoder / *errs.Error
//	good for        routing, infra, 3rd-party:      business + app semantics:
//	                otel, gzip, CORS, access-log,    auth->principal, decode/
//	                panic-recover, timeout,          validate, error localization,
//	                body size-limit (see middleware)  tx injection, translation
//	testing         record an http response         assert on the returned value
//
// The single boundary between them is rest.HandlerFunc.ServeHTTP: it runs the
// typed chain and writes the returned ResponseEncoder via rest.Respond, exactly mirroring
// the stdlib http.HandlerFunc. Because a typed handler is therefore an
// http.Handler, transport middleware always wraps the boundary — transport is
// outermost, application innermost — and that nesting is correct by
// construction: anything that needs the ResponseEncoder is a rest.MidFunc (inside),
// anything that needs raw bytes or is third-party is transport middleware
// (outside). There is no lift from transport into the typed chain (it has no
// ResponseWriter); scope a transport concern to some routes with a sub-group
// instead.
//
// # Method map
//
// The router exposes both layers with parallel names — the bare verb is
// transport, the App suffix is the typed layer:
//
//	concern                       transport (net/http)   application (rest typed)
//	-------                       --------------------   ------------------------
//	add middleware to a group      Use(mw...)             UseApp(mw...)
//	derive a sub-group + middleware With(mw...)           WithApp(mw...)
//	register one handler           Handle / HandleFunc    HandleApp
//	mount a sub-tree               Mount / Route          (shared)
//
// New(appMids...) seeds the global application middleware. Use/UseApp mutate the
// current group and apply to handlers registered afterwards; With/WithApp return
// a derived group and never affect the parent. A single group may serve typed
// (HandleApp) and standard (Handle) handlers side by side.
//
// # Ordering
//
// On a request, layers run outermost first. For a typed route the order is:
//
//	transport (Use/With)  ->  app-global (New)  ->  app-group (UseApp/WithApp)
//	  ->  app-route (HandleApp mids)  ->  handler
//
// Application middleware added later stays inside the earlier ones, so a global
// localize-errors keeps wrapping a group's auth and can localize the error that
// auth returns. Register the transport stack panic-first, then trace, then the
// loggers and guards (see rest/mid). A raw handler registered with Handle runs the
// transport stack but no application middleware.
//
// # Recipes
//
// Assemble the tree once (e.g. in an Install function) and hand features a
// rest.Handle seam (router.HandleApp) so they stay router-agnostic.
//
// Global cross-cutting — transport for infra, app for request semantics:
//
//	r := router.New(reqctxMid, localizeErrMid)                      // application: every typed route
//	r.Use(middleware.Panics(log), middleware.TraceRequest(tracer),  // transport: every route incl. debug
//		middleware.AccessLog(log))
//
// A business sub-group with guards that must not touch debug/pprof routes (a
// request timeout would cut off a long profile), then features register on it:
//
//	api := r.With(middleware.Timeout(5*time.Second), middleware.SizeLimit(1<<20)) // transport sub-group
//	d.WidgetHandler(ctx).Routes(api.HandleApp)                      // feature owns its routes
//	auditrest.NewHandlers(d.AuditLog(ctx)).Routes(api.HandleApp)
//
// Per-route auth (mixed access within one feature — reads public, writes guarded)
// passes app middleware to the writes only:
//
//	authMW := []rest.MidFunc{auth.AuthenticateApp(v), auth.RequireRoleApp("admin")}
//	handle("GET /widgets", h.list)                    // public
//	handle("POST /widgets", h.create, authMW...)      // guarded
//
// A uniformly-protected group uses WithApp instead of repeating per-route:
//
//	admin := api.WithApp(auth.AuthenticateApp(v), auth.RequireRoleApp("admin"))
//	admin.HandleApp("POST /settings", h.update)       // all admin routes guarded
//
// Standard and typed handlers on the same router — a file server next to a typed
// API, or a third-party handler behind transport-only middleware:
//
//	r.Handle("GET /assets/", http.FileServer(http.FS(assets)))
//	r.With(corsMW).Handle("POST /webhook", webhookHandler) // 3rd-party http.Handler
//
// Typed handlers also drop onto a foreign router with no adapter, because a
// chained HandlerFunc is an http.Handler:
//
//	mux := http.NewServeMux()
//	mux.Handle("POST /widgets", rest.ChainMiddleware(h.create, auth.AuthenticateApp(v)))
//
// # Best practices
//
//   - Choose the layer by need, not habit: needs the ResponseEncoder / returns *errs.Error
//     / injects into ctx for handlers -> application (UseApp/WithApp/per-route);
//     generic, third-party, or must wrap the encoded bytes -> transport (Use/With).
//   - Keep features router-agnostic: a Routes method takes a rest.Handle
//     (router.HandleApp), never the *Router. Composition (mounting, grouping,
//     middleware) lives in one Install function.
//   - Put global localize/format-error middleware in New so it stays outermost of
//     the app layer and can localize errors returned by deeper auth/validation.
//   - Keep request timeouts and size limits off debug/probe routes by applying
//     them to a business sub-group (With), not the root.
//   - Prefer per-route rest.MidFunc when access is mixed inside one feature;
//     prefer WithApp when an entire group shares the same guard.
//   - Don't try to run a transport middleware inside the typed chain; if it needs
//     the ResponseEncoder it belongs as a rest.MidFunc, otherwise scope it with a
//     sub-group so it wraps the boundary.
//
// # API
//
// Every concern is a transport/application pair (see the method map above) — the
// bare verb speaks net/http, the App suffix speaks the typed rest layer. The two
// surfaces live in sibling files (http.go, app.go) around the core type in
// router.go:
//
//   - New(appMids ...rest.MidFunc): build a Router; appMids wrap every HandleApp
//     route, outermost first.
//   - Use / With(mws ...Middleware): add transport middleware to this group (Use,
//     mutating, applies to later routes) or to a derived sub-router (With,
//     immutable). Both shadow the embedded routegroup methods so the transport
//     surface stays visible on the type.
//   - Handle / HandleFunc(pattern, h, mws ...Middleware): mount a raw net/http
//     handler with optional per-route transport middleware. With no mws they
//     behave exactly like the embedded routegroup methods they shadow.
//   - UseApp / WithApp(mw ...rest.MidFunc): the typed twins of Use / With.
//   - HandleApp(pattern, rest.HandlerFunc, mids ...rest.MidFunc): mount a typed
//     handler; per-route mids sit inside appMids, the result registers as an
//     http.Handler and is encoded via rest.Respond.
//   - Mount(pattern) / Route(fn): compose sub-routers (a path prefix, or a
//     callback-scoped group); both carry transport + application middleware.
//   - Other embedded *routegroup.Bundle methods (HandleFiles, HandleRoot, ...)
//     remain available for raw net/http use.
package router
