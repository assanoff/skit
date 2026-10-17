package httpmw

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/retry"
)

// fastBackoff keeps retries near-instant so tests don't sleep.
func fastBackoff(maxAttempts int) retry.Backoff {
	return retry.Backoff{Base: time.Millisecond, Max: 2 * time.Millisecond, MaxAttempts: maxAttempts}
}

func TestRetryEventuallySucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, RetryConfig{
		Backoff: fastBackoff(5),
		Rand:    func() float64 { return 0 },
	})}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

func TestRetryExhaustsAndReturnsLastResponse(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, RetryConfig{
		Backoff: fastBackoff(3),
		Rand:    func() float64 { return 0 },
	})}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3 (MaxAttempts)", got)
	}
}

func TestRetryNonRetryableStatusNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, RetryConfig{Backoff: fastBackoff(5)})}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (400 is not retryable)", got)
	}
}

func TestRetryReplaysBody(t *testing.T) {
	var calls atomic.Int32
	var bodies []string
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, RetryConfig{
		Backoff: fastBackoff(3),
		Rand:    func() float64 { return 0 },
	})}

	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{"k":"v"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if len(bodies) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(bodies))
	}
	for i, b := range bodies {
		if b != `{"k":"v"}` {
			t.Fatalf("attempt %d body = %q, want the original payload", i+1, b)
		}
	}
}

func TestRetryHonorsRetryAfterSeconds(t *testing.T) {
	var first atomic.Bool
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !first.Swap(true) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Max clamps the 1s Retry-After down so the test stays fast; the point is
	// that the header path is taken rather than the backoff delay.
	client := &http.Client{Transport: NewRetryTransport(srv.Client().Transport, RetryConfig{
		Backoff: retry.Backoff{Base: time.Hour, Max: 5 * time.Millisecond, MaxAttempts: 3},
		Rand:    func() float64 { return 0 },
	})}

	start := time.Now()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Backoff.Base is an hour; if the header weren't honored (and clamped to
	// Max), this would have blocked far longer than a few ms.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waited %v, expected the clamped Retry-After (~5ms)", elapsed)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	if d, ok := retryAfter("2"); !ok || d != 2*time.Second {
		t.Fatalf("delta-seconds: got (%v,%v), want (2s,true)", d, ok)
	}
	if _, ok := retryAfter(""); ok {
		t.Fatalf("empty: want ok=false")
	}
	if _, ok := retryAfter("garbage"); ok {
		t.Fatalf("garbage: want ok=false")
	}
	if d, ok := retryAfter("-5"); ok || d != 0 {
		t.Fatalf("negative: got (%v,%v), want (0,false)", d, ok)
	}
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	if d, ok := retryAfter(future); !ok || d <= 0 {
		t.Fatalf("http-date: got (%v,%v), want a positive duration", d, ok)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if d, ok := retryAfter(past); !ok || d != 0 {
		t.Fatalf("past http-date: got (%v,%v), want (0,true)", d, ok)
	}
}

// Each attempt carries its resend count: 0, then 1, 2 for the retries.
func TestRetryRecordsResendCount(t *testing.T) {
	is := is.New(t)
	var counts []int
	next := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		counts = append(counts, ResendCount(r.Context()))
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       http.NoBody,
			Request:    r,
		}, nil
	})
	rt := NewRetryTransport(next, RetryConfig{Backoff: fastBackoff(3)})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://upstream/x", nil)
	resp, err := rt.RoundTrip(req)
	is.NoErr(err)
	_ = resp.Body.Close()

	is.Equal(counts, []int{0, 1, 2})
	is.Equal(ResendCount(req.Context()), 0) // the caller's request is untouched
}
