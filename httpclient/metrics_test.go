package httpclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/assanoff/skit/httpclient"
	"github.com/assanoff/skit/httpmw"
	"github.com/assanoff/skit/retry"
)

// Every attempt of a retried call is recorded with its own status code.
func TestMetricsPerAttempt(t *testing.T) {
	is := is.New(t)
	var calls atomic.Int32
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	reg := prometheus.NewRegistry()
	c, err := httpclient.New(httpclient.Config{
		Name:    "billing",
		Metrics: reg,
		Retry: &httpmw.RetryConfig{
			Backoff: retry.Backoff{Base: time.Millisecond, MaxAttempts: 3},
		},
		Base: srv.Client().Transport,
	})
	is.NoErr(err)

	get(t, c, srv.URL)

	want := `
# HELP skit_http_client_requests_total Attempts of HTTP requests, by client, method, status code and error type.
# TYPE skit_http_client_requests_total counter
skit_http_client_requests_total{client="billing",error_type="",http_request_method="GET",http_response_status_code="200"} 1
skit_http_client_requests_total{client="billing",error_type="",http_request_method="GET",http_response_status_code="503"} 2
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_http_client_requests_total"))
	n, err := testutil.GatherAndCount(reg, "skit_http_client_request_duration_seconds")
	is.NoErr(err)
	is.Equal(n, 1) // one duration series per client
}

// failing is a transport whose every request fails with err.
type failing struct{ err error }

func (f failing) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

// A request that got no response is recorded with its error type and no
// status code.
func TestMetricsTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "canceled", err: context.Canceled, want: "canceled"},
		{
			name: "wrapped_deadline",
			err:  fmt.Errorf("dial: %w", context.DeadlineExceeded),
			want: "timeout",
		},
		{name: "net_timeout", err: timeoutError{}, want: "timeout"},
		{name: "unclassified", err: errors.New("connection refused"), want: "_OTHER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			reg := prometheus.NewRegistry()
			c, err := httpclient.New(httpclient.Config{Metrics: reg, Base: failing{err: tt.err}})
			is.NoErr(err)

			req, err := http.NewRequestWithContext(t.Context(), "QUERY", "http://upstream/", http.NoBody)
			is.NoErr(err)
			is.True(httpclient.DoJSON(c, req, nil) != nil) // the transport error

			want := fmt.Sprintf(`
# HELP skit_http_client_requests_total Attempts of HTTP requests, by client, method, status code and error type.
# TYPE skit_http_client_requests_total counter
skit_http_client_requests_total{client="default",error_type=%q,http_request_method="_OTHER",http_response_status_code=""} 1
`, tt.want)
			err = testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_http_client_requests_total")
			is.NoErr(err) // the error type of tt.err
		})
	}
}

// timeoutError is a net.Error that timed out, as a dial or read timeout is.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

func get(t *testing.T, c *http.Client, url string) {
	t.Helper()
	is := is.New(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	is.NoErr(err)
	resp, err := c.Do(req)
	is.NoErr(err)
	_ = resp.Body.Close()
}
