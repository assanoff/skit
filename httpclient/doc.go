// Package httpclient builds configured *http.Client values for calling upstream
// services, and helps consume their JSON responses. It is the transport half of
// a typed service client (see `skit add http-client`): the generated client
// package builds one of these in its constructor, then its methods assemble
// requests and decode responses through DoJSON.
//
// The transport is composed from httpmw middlewares — retry (5xx/429 +
// Retry-After), a User-Agent, an idempotency key, arbitrary headers — and, when
// OAuth2 is configured, an OAuth2 client_credentials bearer. Composition order
// (httpmw.Chain) puts retry outermost and auth innermost, so a retried attempt
// re-attaches the current token.
//
// # Usage
//
//	hc, err := httpclient.New(httpclient.Config{
//		UserAgent:      "billing-client",
//		Retry:          &httpmw.RetryConfig{Backoff: retry.Backoff{MaxAttempts: 3}},
//		TracerProvider: d.TracerProvider,
//	})
//	if err != nil {
//		return err
//	}
//	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
//	err = httpclient.DoJSON(hc, req, &out)
//
// # Tracing
//
// Every attempt gets an OpenTelemetry client span (otelhttp) — a retried call
// shows each try — and sends its traceparent, so the upstream continues the
// caller's trace. The span sits below retry and the header decorations and
// above OAuth2: it sees the final headers, and the token fetch gets no span. A
// retry carries http.request.resend_count (httpmw.ResendCount). url.full has no
// user info, and the values of the signature query keys of the semantic
// conventions (AWSAccessKeyId, Signature, sig, X-Goog-Signature) read REDACTED.
// Pass the request context: the span is a child of the span in it.
//
// # Config
//
//   - Timeout: per-request timeout, retries included (default DefaultTimeout).
//   - Retry: retry 5xx/429 with Retry-After; nil disables retries.
//   - OAuth2: client_credentials bearer; nil disables it.
//   - UserAgent, Version, Environment: "product/version (env)" User-Agent.
//   - IdempotencyHeader: per-request idempotency key under this header.
//   - Headers: static headers on every request.
//   - Base: the wire transport (default http.DefaultTransport).
//   - TracerProvider: records the client spans; nil → the global provider.
package httpclient
