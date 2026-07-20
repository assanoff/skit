package httpmw

import (
	"bytes"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/assanoff/skit/retry"
)

// RetryConfig configures a RetryTransport.
type RetryConfig struct {
	// Backoff drives the delay between attempts and the total-attempt budget via
	// its MaxAttempts field. MaxAttempts <= 1 disables retries (a single try).
	Backoff retry.Backoff
	// Statuses are the response status codes that trigger a retry. When empty it
	// defaults to {429 Too Many Requests, 503 Service Unavailable}.
	Statuses []int
	// DisableRetryAfter turns off honoring the server's Retry-After header. By
	// default (false) a Retry-After value (delta-seconds or HTTP-date) on a
	// retryable response is used in preference to the backoff delay, clamped to
	// Backoff.Max when Max > 0.
	DisableRetryAfter bool
	// Rand returns a pseudo-random value in [0,1) used for backoff jitter. It
	// defaults to math/rand/v2; override it in tests for determinism.
	Rand func() float64
}

// RetryTransport is an http.RoundTripper that retries retryable responses
// according to its RetryConfig. The zero value is not usable; build it with
// NewRetryTransport.
type RetryTransport struct {
	next     http.RoundTripper
	statuses map[int]bool
	cfg      RetryConfig
}

// NewRetryTransport wraps next (or http.DefaultTransport when nil) with retry
// behavior. The result is safe for concurrent use if next is.
func NewRetryTransport(next http.RoundTripper, cfg RetryConfig) *RetryTransport {
	if next == nil {
		next = http.DefaultTransport
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Float64
	}
	statuses := cfg.Statuses
	if len(statuses) == 0 {
		statuses = []int{http.StatusTooManyRequests, http.StatusServiceUnavailable}
	}
	set := make(map[int]bool, len(statuses))
	for _, s := range statuses {
		set[s] = true
	}
	return &RetryTransport{next: next, statuses: set, cfg: cfg}
}

// RoundTrip executes the request, retrying on configured statuses until the
// attempt budget is exhausted or the context is canceled. The request body is
// rewound between attempts.
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Per the http.RoundTripper contract we must not mutate the caller's
	// request; clone it so body rewinding and per-attempt Body assignment touch
	// only our copy.
	req = req.Clone(req.Context())

	getBody, err := rewindable(req)
	if err != nil {
		return nil, err
	}

	maxAttempts := max(t.cfg.Backoff.MaxAttempts, 1)

	var resp *http.Response
	for attempt := 1; ; attempt++ {
		if getBody != nil {
			body, bErr := getBody()
			if bErr != nil {
				return nil, bErr
			}
			req.Body = body
		}

		resp, err = t.next.RoundTrip(req)
		// A transport error or a non-retryable status ends the loop immediately.
		if err != nil || !t.statuses[resp.StatusCode] || attempt >= maxAttempts {
			return resp, err
		}

		delay := t.delay(attempt, resp)
		// Drain and close so the connection can be reused for the retry.
		drain(resp)

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
		}
	}
}

// delay picks the wait before the next attempt: the Retry-After header when
// present and honored, otherwise the backoff delay for this attempt.
func (t *RetryTransport) delay(attempt int, resp *http.Response) time.Duration {
	if !t.cfg.DisableRetryAfter {
		if d, ok := retryAfter(resp.Header.Get("Retry-After")); ok {
			if maxDelay := t.cfg.Backoff.Max; maxDelay > 0 && d > maxDelay {
				d = maxDelay
			}
			return d
		}
	}
	return t.cfg.Backoff.NextWithRand(attempt, t.cfg.Rand())
}

// rewindable returns a function that yields a fresh body reader for each
// attempt, or nil when the request has no body. It buffers the body in memory
// when the request cannot otherwise be replayed.
func rewindable(req *http.Request) (func() (io.ReadCloser, error), error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.GetBody != nil {
		return req.GetBody, nil
	}
	buf, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}
	return func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf)), nil
	}, nil
}

// drain consumes and closes a response body so the underlying connection is
// returned to the pool for reuse.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// retryAfter parses an RFC 7231 Retry-After value, either delta-seconds or an
// HTTP-date. A past or unparseable date yields (0, false).
func retryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(v); err == nil {
		d := max(time.Until(when), 0)
		return d, true
	}
	return 0, false
}
