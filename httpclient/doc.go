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
//		Name:           "billing",
//		Metrics:        d.Metrics,
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
// # Metrics
//
// With Config.Metrics set, every attempt is recorded under the client's Name:
//   - skit_http_client_requests_total by client, http_request_method,
//     http_response_status_code and error_type;
//   - skit_http_client_request_duration_seconds by client, with the span of
//     the attempt as the exemplar.
//
// A response is recorded with its status code and an empty error_type; a
// request that got no response with an empty status code and error_type
// canceled, timeout or _OTHER. A method the HTTP semantic conventions do not
// know is recorded as _OTHER.
//
// # Config
//
//   - Name: the client label of the metrics (default "default").
//   - Metrics: the registry of the request metrics; nil disables them.
//   - Timeout: per-request timeout, retries included (default DefaultTimeout).
//   - Retry: retry 5xx/429 with Retry-After; nil disables retries.
//   - OAuth2: client_credentials bearer; nil disables it.
//   - UserAgent, Version, Environment: "product/version (env)" User-Agent.
//   - IdempotencyHeader: per-request idempotency key under this header.
//   - Headers: static headers on every request.
//   - Base: the wire transport (default http.DefaultTransport).
//   - TracerProvider: records the client spans; nil → the global provider.
package httpclient
