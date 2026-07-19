package httpmw

import (
	"net/http"

	"github.com/google/uuid"
)

// Middleware wraps an http.RoundTripper, returning one that adds behavior around
// next. It is the composition unit for outbound HTTP: retries, header injection,
// auth, logging. Compose several with Chain.
type Middleware func(next http.RoundTripper) http.RoundTripper

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Chain wraps base (or http.DefaultTransport when nil) with mws, applied so that
// the FIRST middleware is outermost — it sees the request first and the response
// last. So Chain(base, Retry, UserAgent) runs Retry around UserAgent around base:
// a retried attempt re-applies UserAgent (and any auth) each time. Install the
// result as an http.Client's Transport.
func Chain(base http.RoundTripper, mws ...Middleware) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] != nil {
			base = mws[i](base)
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
// when the caller has not already set it. Leaving an existing value intact is
// what makes it retry-safe: a retried request carries the same key, so the
// server dedupes repeated attempts of one logical call. A blank header defaults
// to "Idempotency-Key".
func IdempotencyKey(header string) Middleware {
	if header == "" {
		header = "Idempotency-Key"
	}
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get(header) == "" {
				r = cloneReq(r)
				r.Header.Set(header, uuid.NewString())
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
