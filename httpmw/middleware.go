package httpmw

import (
	"cmp"
	"net/http"
	"slices"
	"uuid"
)

// Middleware wraps an http.RoundTripper, returning one that adds behavior around
// next. It is the composition unit for outbound HTTP: retries, header injection,
// auth, logging. Compose several with Chain.
type Middleware func(next http.RoundTripper) http.RoundTripper

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// Chain wraps base (or http.DefaultTransport when nil) with mws, applied so that
// the FIRST middleware is outermost — it sees the request first and the response
// last. So Chain(base, Retry, UserAgent) runs Retry around UserAgent around base:
// a retried attempt re-applies UserAgent (and any auth) each time. Install the
// result as an http.Client's Transport.
func Chain(base http.RoundTripper, mws ...Middleware) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	for _, mw := range slices.Backward(mws) {
		if mw != nil {
			base = mw(base)
		}
	}
	return base
}

// Retry is the RetryTransport as a Middleware, so it composes in a Chain. It is
// equivalent to NewRetryTransport(next, cfg) for the wrapped transport.
func Retry(cfg RetryConfig) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return NewRetryTransport(next, cfg)
	}
}

// SetHeader returns a Middleware that sets key: value on every outbound request,
// overwriting any existing value. A blank key is a no-op.
func SetHeader(key, value string) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if key != "" {
				r = cloneReq(r)
				r.Header.Set(key, value)
			}
			return next.RoundTrip(r)
		})
	}
}

// UserAgent returns a Middleware that sets the User-Agent header to
// "product/version (env)", skipping any empty parts. A blank product is a no-op
// (User-Agent left untouched).
func UserAgent(product, version, env string) Middleware {
	ua := buildUserAgent(product, version, env)
	if ua == "" {
		return func(next http.RoundTripper) http.RoundTripper { return next }
	}
	return SetHeader("User-Agent", ua)
}

// IdempotencyKey returns a Middleware that sets header to a fresh UUID v4 only
// when the caller has not already set it. A blank header defaults to
// "Idempotency-Key".
//
// Put it outside Retry in a Chain: the key is set once, and every attempt of
// the call carries it, so the server dedupes them. Below Retry each attempt
// gets a key of its own. A call the caller repeats above the client keeps one
// key only when the caller sets the header itself: an existing value is left
// intact.
func IdempotencyKey(header string) Middleware {
	header = cmp.Or(header, "Idempotency-Key")
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get(header) == "" {
				r = cloneReq(r)
				r.Header.Set(header, uuid.New().String())
			}
			return next.RoundTrip(r)
		})
	}
}

// cloneReq shallow-clones a request and its header so a middleware can mutate
// headers without touching the caller's request (RoundTrippers must not modify
// the request they are given).
func cloneReq(r *http.Request) *http.Request {
	r2 := r.Clone(r.Context())
	return r2
}

// buildUserAgent assembles "product/version (env)" from the non-empty parts.
func buildUserAgent(product, version, env string) string {
	if product == "" {
		return ""
	}
	ua := product
	if version != "" {
		ua += "/" + version
	}
	if env != "" {
		ua += " (" + env + ")"
	}
	return ua
}
