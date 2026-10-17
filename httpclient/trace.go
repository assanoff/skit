package httpclient

import (
	"cmp"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/httpmw"
	skotel "github.com/assanoff/skit/otel"
)

// clientSpan opens a client span per attempt with otelhttp and writes its
// traceparent into the request; spanAttrs below it completes the span.
func clientSpan(tp trace.TracerProvider) httpmw.Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(spanAttrs{next: next},
			otelhttp.WithTracerProvider(cmp.Or(tp, otel.GetTracerProvider())),
			otelhttp.WithPropagators(skotel.Propagator()),
			// HTTP client metrics come from package metrics; otelhttp records
			// only spans.
			otelhttp.WithMeterProvider(noop.NewMeterProvider()),
		)
	}
}

// spanAttrs sets what otelhttp does not on the client span of an attempt: the
// resend count of a retry, and url.full with the values of signature query
// keys replaced by REDACTED.
type spanAttrs struct {
	next http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (s spanAttrs) RoundTrip(r *http.Request) (*http.Response, error) {
	span := trace.SpanFromContext(r.Context())
	if n := httpmw.ResendCount(r.Context()); n > 0 {
		span.SetAttributes(semconv.HTTPRequestResendCount(n))
	}
	if u, ok := redactURL(r.URL); ok {
		span.SetAttributes(semconv.URLFull(u))
	}
	return s.next.RoundTrip(r)
}

// redactURL returns u as url.full carries it — without user info, with the
// values of signature query keys replaced by REDACTED — and whether a value
// was replaced. The query keeps its order and encoding. Without a replacement
// the url.full otelhttp set stands.
func redactURL(u *url.URL) (string, bool) {
	params := strings.Split(u.RawQuery, "&")
	redacted := false
	for i, p := range params {
		k, _, found := strings.Cut(p, "=")
		key, err := url.QueryUnescape(k)
		if err != nil || !found || !signatureKey(key) {
			continue
		}
		params[i] = k + "=REDACTED"
		redacted = true
	}
	if !redacted {
		return "", false
	}
	c := *u
	c.User = nil
	c.RawQuery = strings.Join(params, "&")
	return c.String(), true
}

// signatureKey reports whether the value of query key is a credential the
// semantic conventions hide in url.full.
func signatureKey(key string) bool {
	switch key {
	case "AWSAccessKeyId", "Signature", "sig", "X-Goog-Signature":
		return true
	}
	return false
}
