package httpmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/httpmw"
)

// capture records the last request headers a server saw.
func capture(t *testing.T, seen *http.Header) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSetHeaderAndUserAgent(t *testing.T) {
	is := is.New(t)
	var seen http.Header
	srv := capture(t, &seen)

	client := &http.Client{Transport: httpmw.Chain(nil,
		httpmw.UserAgent("svc", "1.2.3", "prod"),
		httpmw.SetHeader("X-Trace", "abc"),
	)}
	resp, err := client.Get(srv.URL)
	is.NoErr(err)
	_ = resp.Body.Close()

	is.Equal(seen.Get("User-Agent"), "svc/1.2.3 (prod)") // product/version (env)
	is.Equal(seen.Get("X-Trace"), "abc")
}

func TestIdempotencyKeySetWhenAbsentKeptWhenPresent(t *testing.T) {
	is := is.New(t)
	var seen http.Header
	srv := capture(t, &seen)
	client := &http.Client{Transport: httpmw.Chain(nil, httpmw.IdempotencyKey("X-Idempotency-Key"))}

	// Absent -> a key is injected.
	resp, err := client.Get(srv.URL)
	is.NoErr(err)
	_ = resp.Body.Close()
	is.True(seen.Get("X-Idempotency-Key") != "")

	// Present -> the caller's value is kept.
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("X-Idempotency-Key", "caller-key")
	resp, err = client.Do(req)
	is.NoErr(err)
	_ = resp.Body.Close()
	is.Equal(seen.Get("X-Idempotency-Key"), "caller-key")
}

func TestChainDoesNotMutateCallerRequest(t *testing.T) {
	is := is.New(t)
	var seen http.Header
	srv := capture(t, &seen)
	client := &http.Client{Transport: httpmw.Chain(nil, httpmw.SetHeader("X-Added", "1"))}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	is.NoErr(err)
	_ = resp.Body.Close()

	is.Equal(seen.Get("X-Added"), "1")      // server saw the header
	is.Equal(req.Header.Get("X-Added"), "") // caller's request was not mutated
}

func TestChainOrderIsOutermostFirst(t *testing.T) {
	is := is.New(t)
	var order []string

	mw := func(name string) httpmw.Middleware {
		return func(next http.RoundTripper) http.RoundTripper {
			return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				order = append(order, name)
				return next.RoundTrip(r)
			})
		}
	}
	var seen http.Header
	srv := capture(t, &seen)
	client := &http.Client{Transport: httpmw.Chain(nil, mw("first"), mw("second"))}

	resp, err := client.Get(srv.URL)
	is.NoErr(err)
	_ = resp.Body.Close()

	is.Equal(order, []string{"first", "second"}) // first middleware runs outermost
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
