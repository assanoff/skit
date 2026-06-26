// Package rest is a tiny typed-handler layer over net/http.
//
// A HandlerFunc returns a ResponseEncoder — a value that knows how to serialize itself
// — instead of writing to the ResponseWriter directly. This keeps response
// encoding and error handling in one place (Respond), makes handlers trivial to
// unit-test (assert on the returned value, not a recorded response), and lets a
// handler signal failure by returning an *errs.Error, which Respond maps to the
// right HTTP status. Mount these handlers with the router package.
//
// HandlerFunc implements http.Handler (via ServeHTTP, which runs the handler and
// calls Respond), so this typed application layer plugs into any net/http router
// with no adapter: it is the single boundary to the transport layer, and
// net/http middleware (func(http.Handler) http.Handler) always wraps it from the
// outside. See the router package for the two-layer model and the parallel
// Use/UseApp, With/WithApp, Handle/HandleApp method pairs.
//
// # ResponseEncoders and status
//
// A ResponseEncoder reports its body and content type; if it also implements an
// HTTPStatus() int method it sets the response status (otherwise 200, or 204 for
// a nil ResponseEncoder). JSON and JSONStatus wrap any value as a JSON ResponseEncoder, and
// *errs.Error is itself a ResponseEncoder, so a handler returns one uniform type for
// both success and failure.
//
// # Usage
//
//	func getWidget(ctx context.Context, r *http.Request) rest.ResponseEncoder {
//		var in CreateWidget
//		if err := rest.Decode(r, &in); err != nil {
//			return err.(*errs.Error) // InvalidArgument -> 400
//		}
//		w, err := store.Find(ctx, in.ID)
//		if err != nil {
//			return errs.New(errs.NotFound, err) // -> 404
//		}
//		return rest.JSON(w) // -> 200 application/json
//	}
//
//	// Compose cross-cutting behavior, outermost first:
//	h := rest.ChainMiddleware(getWidget, authMid, logMid)
//	resp := h(ctx, r)
//	_ = rest.Respond(ctx, w, resp)
//
// # API
//
//   - HandlerFunc: func(ctx, *http.Request) ResponseEncoder — the typed handler shape;
//     also an http.Handler (ServeHTTP), the boundary to the net/http layer.
//   - MidFunc / ChainMiddleware: wrap a HandlerFunc; mw[0] is the outermost
//     layer. ChainMiddleware returns a HandlerFunc, so a chained typed handler is
//     itself an http.Handler: mux.Handle(p, rest.ChainMiddleware(h, mids...)).
//   - Chain: collapse several MidFunc into one reusable MidFunc (same order; nil
//     elements skipped, so empty/all-nil is the identity). Use it to name a
//     bundle and pass it where a single MidFunc is expected; the []MidFunc + spread
//     form stays the primary way to accumulate middleware on a router group.
//   - Respond: write a ResponseEncoder to an http.ResponseWriter (nil -> 204; encode
//     failure -> 500 with a generic body; cancelled ctx -> returns ctx.Err).
//   - JSON / JSONStatus: wrap a value as a JSON ResponseEncoder (status 200, or
//     explicit).
//   - Decode: read and validate a JSON request body (DisallowUnknownFields +
//     errs.Check), returning an *errs.Error (InvalidArgument) on bad input.
//   - NoResponse: a ResponseEncoder telling Respond to write nothing (the handler
//     already wrote the response — redirect, stream, hijack).
//   - Param: read a path parameter (thin wrapper over http.Request.PathValue).
//
// To supervise the router as a server, wrap it with httpserver (or the
// server package's HTTP brick, Name "rest-server") so it runs and drains under a
// worker.Group alongside the gRPC and status servers — REST has no server type
// of its own; it is just an http.Handler.
package rest
