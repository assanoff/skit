// Package httpmw provides outbound HTTP-client middleware: composable
// http.RoundTripper wrappers for resilient calls to upstream services.
//
// RetryTransport retries throttled/unavailable responses (429 and 503 by
// default) using the shared worker.Backoff policy, honoring a server-supplied
// Retry-After header (RFC 7231 §7.1.3) when present. It rewinds the request body
// between attempts (buffering in memory when the body is not otherwise
// replayable) and aborts as soon as the request context is cancelled. Wrap it
// around any http.RoundTripper (or http.DefaultTransport when nil) and install
// it on an http.Client.
//
// # Usage
//
//	client := &http.Client{
//		Transport: httpmw.NewRetryTransport(nil, httpmw.RetryConfig{
//			Backoff: worker.Backoff{
//				Base: 200 * time.Millisecond, Max: 5 * time.Second,
//				MaxAttempts: 4, Jitter: 0.2,
//			},
//		}),
//	}
//	resp, err := client.Get("https://upstream/api")
//
// # Config
//
// RetryConfig fields:
//
//   - Backoff (worker.Backoff): inter-attempt delay and the total-attempt budget
//     via MaxAttempts. MaxAttempts <= 1 disables retries (a single try).
//   - Statuses ([]int): status codes that trigger a retry. Empty defaults to
//     {429 Too Many Requests, 503 Service Unavailable}.
//   - DisableRetryAfter (bool): when false (default), a retryable response's
//     Retry-After value (delta-seconds or HTTP-date) is preferred over the
//     backoff delay, clamped to Backoff.Max when Max > 0.
//   - Rand (func() float64): source of backoff jitter in [0,1); defaults to
//     math/rand/v2. Override for deterministic tests.
package httpmw
