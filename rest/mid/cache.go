package mid

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
)

// CacheControl returns application middleware that sets the Cache-Control (and,
// when given, Vary) response header for successful responses. It is an
// app-developer choice attached per handler or group — handle("GET /x", h.get,
// mid.CacheControl(60)) — not global infrastructure. maxAge is in seconds; a
// non-positive maxAge sets no Cache-Control. Error and 204 responses are left
// uncached.
//
// It sets a header via rest.GetWriter but never calls WriteHeader, so Respond
// remains the single writer of status and body and the middleware composes in
// any order with others (e.g. ETag). With no ResponseWriter in context (a
// handler invoked directly, off the ServeHTTP boundary) it is a no-op.
func CacheControl(maxAge int, varyHeaders ...string) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			resp := next(ctx, r)
			if resp == nil || isError(resp) {
				return resp
			}
			w := rest.GetWriter(ctx)
			if w == nil {
				return resp
			}
			if len(varyHeaders) > 0 {
				w.Header().Set("Vary", strings.Join(varyHeaders, ", "))
			}
			if maxAge > 0 {
				w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge))
			}
			return resp
		}
	}
}

// isError reports whether a ResponseEncoder is an error response (an *errs.Error). Used
// by the caching middleware to skip non-successful responses.
func isError(resp rest.ResponseEncoder) bool {
	_, ok := resp.(*errs.Error)
	return ok
}
