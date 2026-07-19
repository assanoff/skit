package httpclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/httpclient"
)

func TestNewPlainClientSendsRequest(t *testing.T) {
	is := is.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Header.Get("User-Agent"), "svc/1.0 (test)")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c, err := httpclient.New(httpclient.Config{UserAgent: "svc", Version: "1.0", Environment: "test"})
	is.NoErr(err)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	var out struct {
		OK bool `json:"ok"`
	}
	is.NoErr(httpclient.DoJSON(c, req, &out))
	is.True(out.OK) // 2xx body decoded
}

func TestDoJSONNon2xxReturnsStatusError(t *testing.T) {
	is := is.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := httpclient.New(httpclient.Config{})
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)

	err := httpclient.DoJSON(c, req, nil)
	var se *httpclient.StatusError
	is.True(errors.As(err, &se)) // typed status error
	is.Equal(se.StatusCode, http.StatusNotFound)
	is.True(strings.Contains(string(se.Body), "nope")) // body captured
}

func TestDoJSONEmptyBodyOK(t *testing.T) {
	is := is.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // 204, empty body
	}))
	defer srv.Close()

	c, _ := httpclient.New(httpclient.Config{})
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	var out struct {
		X int `json:"x"`
	}
	is.NoErr(httpclient.DoJSON(c, req, &out)) // empty 2xx body is not an error
}

func TestOAuth2ConfigValidation(t *testing.T) {
	is := is.New(t)
	_, err := httpclient.New(httpclient.Config{OAuth2: &httpclient.OAuth2Config{TokenURL: "http://x"}})
	is.True(err != nil) // missing client id/secret rejected
}

// TestOAuth2AttachesBearerToken checks the OAuth2 transport fetches a token from
// the token endpoint and attaches it as a bearer on the business request.
func TestOAuth2AttachesBearerToken(t *testing.T) {
	is := is.New(t)

	var tokenHits int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-123","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	var gotAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer apiSrv.Close()

	c, err := httpclient.New(httpclient.Config{
		OAuth2: &httpclient.OAuth2Config{
			TokenURL:     tokenSrv.URL,
			ClientID:     "id",
			ClientSecret: "secret",
			Scopes:       []string{"read"},
		},
	})
	is.NoErr(err)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, apiSrv.URL, nil)
	is.NoErr(httpclient.DoJSON(c, req, nil))

	is.Equal(gotAuth, "Bearer tok-123") // bearer attached from the token endpoint
	is.True(tokenHits >= 1)             // token was fetched
}
