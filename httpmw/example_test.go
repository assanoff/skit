package httpmw_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	"github.com/assanoff/skit/httpmw"
	"github.com/assanoff/skit/retry"
)

// Chain composes the transport of a client, the first middleware outermost:
// the idempotency key is set once, above the retry, so every attempt carries
// it; the User-Agent below the retry is set on each attempt.
func Example() {
	var (
		mu   sync.Mutex
		keys []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		n := len(keys)
		fmt.Println("attempt", n, r.UserAgent(), "same key:", keys[0] == keys[n-1])
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	client := &http.Client{Transport: httpmw.Chain(nil,
		httpmw.IdempotencyKey(""),
		httpmw.Retry(httpmw.RetryConfig{
			Backoff: retry.Backoff{Base: time.Millisecond, MaxAttempts: 3},
		}),
		httpmw.UserAgent("billing", "1.4.0", "prod"),
	)}
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, upstream.URL+"/payments", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	fmt.Println(resp.StatusCode)
	// Output:
	// attempt 1 billing/1.4.0 (prod) same key: true
	// attempt 2 billing/1.4.0 (prod) same key: true
	// attempt 3 billing/1.4.0 (prod) same key: true
	// 201
}

// roundTripFunc makes a function an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// A transport below the retry reads the number of the attempt with
// ResendCount: 0 for the first, n for the n-th retry.
func ExampleResendCount() {
	var attempt atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempt.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	logAttempt := func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp, err := next.RoundTrip(r)
			if err == nil {
				fmt.Println("resend", httpmw.ResendCount(r.Context()), resp.StatusCode)
			}
			return resp, err
		})
	}
	client := &http.Client{Transport: httpmw.Chain(nil,
		httpmw.Retry(httpmw.RetryConfig{Backoff: retry.Backoff{MaxAttempts: 2}}),
		logAttempt,
	)}

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, upstream.URL, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	// Output:
	// resend 0 429
	// resend 1 200
}
