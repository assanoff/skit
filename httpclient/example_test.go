package httpclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/assanoff/skit/httpclient"
	"github.com/assanoff/skit/httpmw"
	"github.com/assanoff/skit/retry"
)

type invoice struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

// New builds the client of an upstream from one Config; DoJSON sends a
// request and decodes the JSON of a 2xx response.
func Example() {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("upstream:", r.UserAgent())
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"inv-7","amount":1200}`)
	}))
	defer upstream.Close()

	hc, err := httpclient.New(httpclient.Config{
		Name:        "billing",
		UserAgent:   "shop",
		Version:     "1.4.0",
		Environment: "prod",
		Retry: &httpmw.RetryConfig{
			Backoff: retry.Backoff{Base: 100 * time.Millisecond, MaxAttempts: 3},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, upstream.URL+"/invoices/inv-7", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	var inv invoice
	err = httpclient.DoJSON(hc, req, &inv)
	fmt.Println(inv.ID, inv.Amount, err)
	// Output:
	// upstream: shop/1.4.0 (prod)
	// inv-7 1200 <nil>
}

// A response that is not 2xx is a *StatusError with the status and the start
// of the body, so the caller branches on the status.
func ExampleDoJSON() {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"not_found"}`, http.StatusNotFound)
	}))
	defer upstream.Close()

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, upstream.URL+"/invoices/inv-0", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	err = httpclient.DoJSON(upstream.Client(), req, &invoice{})
	if se, ok := errors.AsType[*httpclient.StatusError](err); ok {
		fmt.Println(se.StatusCode)
	}
	fmt.Println(err)
	// Output:
	// 404
	// http 404 Not Found: {"code":"not_found"}
}

// OAuth2Transport fetches a client_credentials token, caches it and sets it
// on every request. Compose it innermost, below the retry, so a retried
// attempt carries the current token.
func ExampleOAuth2Transport() {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"t-1","token_type":"bearer","expires_in":3600}`)
			return
		}
		fmt.Println("upstream:", r.Header.Get("Authorization"))
	}))
	defer upstream.Close()

	auth, err := httpclient.OAuth2Transport(httpclient.OAuth2Config{
		TokenURL:     upstream.URL + "/token",
		ClientID:     "shop",
		ClientSecret: "s3cret",
	}, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	hc := &http.Client{Transport: httpmw.Chain(auth,
		httpmw.Retry(httpmw.RetryConfig{Backoff: retry.Backoff{MaxAttempts: 3}}),
	)}

	for range 2 {
		req, err := http.NewRequestWithContext(context.Background(),
			http.MethodGet, upstream.URL+"/invoices", nil)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(httpclient.DoJSON(hc, req, nil))
	}
	// Output:
	// upstream: Bearer t-1
	// <nil>
	// upstream: Bearer t-1
	// <nil>
}
