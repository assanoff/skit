package rest

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/assanoff/skit/errs"
)

// ResponseEncoder is a value that can serialize itself into a response body.
type ResponseEncoder interface {
	Encode() (data []byte, contentType string, err error)
}

// httpStatuser lets a ResponseEncoder advertise its HTTP status code. ResponseEncoders that do
// not implement it default to 200 (or 204 for a nil ResponseEncoder).
type httpStatuser interface {
	HTTPStatus() int
}

// HandlerFunc handles a request and returns a ResponseEncoder describing the response.
// Returning a non-nil error value (e.g. *errs.Error) is the idiomatic way to
// signal failure — Respond will set the right status.
type HandlerFunc func(ctx context.Context, r *http.Request) ResponseEncoder

// ServeHTTP makes HandlerFunc an http.Handler, mirroring the stdlib
// http.HandlerFunc: it runs the handler and writes the returned ResponseEncoder via
// Respond. This is the single boundary between the typed application layer
// (HandlerFunc/MidFunc, which speak ResponseEncoder) and the net/http transport layer
// (http.Handler, which speaks bytes). Because of it a typed handler — including
// one wrapped by ChainMiddleware, since that returns a HandlerFunc — registers
// directly on any net/http router:
//
//	mux.Handle("POST /widgets", rest.ChainMiddleware(h.create, auth, decode))
//
// Transport middleware (func(http.Handler) http.Handler) therefore always wraps
// the boundary, i.e. runs outside the MidFunc chain; that nesting is correct by
// construction (anything needing the ResponseEncoder is a MidFunc, anything needing raw
// bytes is transport middleware).
func (h HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := WithWriter(r.Context(), w)
	_ = Respond(ctx, w, h(ctx, r))
}

type writerKey struct{}

// WithWriter returns a context carrying the response writer. The boundary
// (HandlerFunc.ServeHTTP) installs it automatically, so application middleware
// reached through the typed chain can recover the ResponseWriter via GetWriter.
func WithWriter(ctx context.Context, w http.ResponseWriter) context.Context {
	return context.WithValue(ctx, writerKey{}, w)
}

// GetWriter returns the ResponseWriter installed by the boundary, or nil when
// absent (e.g. a HandlerFunc invoked directly without ServeHTTP, as in a unit
// test). It is an escape hatch for application middleware that must set response
// headers (Cache-Control, ETag) — concerns the ResponseEncoder cannot express. Ordinary
// handlers never need it: return a ResponseEncoder and let Respond write. A middleware
// that uses it to set headers must NOT also call WriteHeader, so Respond stays
// the single place that writes status and body.
func GetWriter(ctx context.Context) http.ResponseWriter {
	w, _ := ctx.Value(writerKey{}).(http.ResponseWriter)
	return w
}

// MidFunc wraps a HandlerFunc to add cross-cutting behavior.
type MidFunc func(HandlerFunc) HandlerFunc

// Handle registers a typed HandlerFunc under a "METHOD /pattern" route with
// optional per-route middleware. It is the registration seam a feature's Routes
// method receives so the feature need not depend on the router type: a router's
// HandleApp method value is a Handle, and Routes calls it once per endpoint.
// This keeps route registration in the feature while route composition
// (mounting, grouping, global middleware) stays with the router in one Install
// function, which passes router.HandleApp as the Handle.
type Handle func(pattern string, h HandlerFunc, mids ...MidFunc)

// ChainMiddleware wraps h with mw in order, so mw[0] is the outermost layer.
func ChainMiddleware(h HandlerFunc, mw ...MidFunc) HandlerFunc {
	for i := len(mw) - 1; i >= 0; i-- {
		if mw[i] != nil {
			h = mw[i](h)
		}
	}
	return h
}

// Chain collapses several MidFunc into a single one, applied outermost-first
// (Chain(a, b)(h) wraps as a(b(h)) — the same order as ChainMiddleware). nil
// elements are skipped, so an all-nil or empty Chain is a no-op (the identity
// middleware).
//
// Whereas ChainMiddleware applies middleware to a known handler right away, Chain
// seals a group into a reusable, first-class MidFunc value: name a bundle once
// and pass it anywhere a single MidFunc is expected — a per-route slot, another
// Chain, or a router's UseApp/WithApp. The slice form ([]MidFunc + spread) stays
// the primary way to accumulate middleware on a group; Chain is for when a bundle
// must travel as one value.
func Chain(mw ...MidFunc) MidFunc {
	return func(next HandlerFunc) HandlerFunc {
		return ChainMiddleware(next, mw...)
	}
}

// Respond writes resp to w. A nil ResponseEncoder yields 204 No Content. If encoding
// fails, a 500 with a generic error body is written instead.
func Respond(ctx context.Context, w http.ResponseWriter, resp ResponseEncoder) error {
	if ctx.Err() != nil {
		// Client gone; nothing useful to write.
		return ctx.Err()
	}

	if _, ok := resp.(NoResponse); ok {
		// The handler already wrote the response itself.
		return nil
	}

	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	status := http.StatusOK
	if hs, ok := resp.(httpStatuser); ok {
		status = hs.HTTPStatus()
	}

	data, contentType, err := resp.Encode()
	if err != nil {
		return writeError(w, errs.New(errs.Internal, err))
	}

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(status)
	_, err = w.Write(data)
	return err
}

func writeError(w http.ResponseWriter, e *errs.Error) error {
	data, contentType, _ := e.Encode()
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(e.HTTPStatus())
	_, err := w.Write(data)
	return err
}

// jsonResponse is a generic ResponseEncoder for arbitrary values with an explicit status.
type jsonResponse struct {
	value  any
	status int
}

func (j jsonResponse) Encode() ([]byte, string, error) {
	data, err := json.Marshal(j.value)
	return data, "application/json", err
}

func (j jsonResponse) HTTPStatus() int { return j.status }

// JSON wraps v as a JSON ResponseEncoder with status 200.
func JSON(v any) ResponseEncoder { return jsonResponse{value: v, status: http.StatusOK} }

// JSONStatus wraps v as a JSON ResponseEncoder with an explicit status.
func JSONStatus(v any, status int) ResponseEncoder { return jsonResponse{value: v, status: status} }

// NoResponse is a ResponseEncoder that tells Respond to write nothing, for handlers
// that have already written to the ResponseWriter themselves (a redirect, a
// streamed body, a hijacked connection).
type NoResponse struct{}

// Encode implements ResponseEncoder; it produces no body.
func (NoResponse) Encode() ([]byte, string, error) {
	return nil, "", nil
}

// Param returns the named path parameter from the request — a thin wrapper over
// http.Request.PathValue, for symmetry with the other rest helpers.
func Param(r *http.Request, key string) string {
	return r.PathValue(key)
}

// Decode reads a JSON request body into v and validates it with errs.Check.
// It returns an *errs.Error (InvalidArgument) on malformed input.
func Decode(r *http.Request, v any) error {
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return errs.Newf(errs.InvalidArgument, "decode request body: %s", err)
		}
	}
	return errs.Check(v)
}
