package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/auth"
	"github.com/assanoff/skit/auth/internal/mocks"
)

// verifier accepts the token "good" as the subject u-1 and rejects the rest.
func verifier() *mocks.VerifierMock {
	return &mocks.VerifierMock{
		VerifyFunc: func(_ context.Context, token string) (*auth.Principal, error) {
			if token != "good" {
				return nil, errors.New("bad token")
			}
			return &auth.Principal{Subject: "u-1"}, nil
		},
	}
}

// subject answers 200 with the subject of the Principal, or "" without one.
func subject() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.PrincipalFromContext(r.Context())
		if !ok {
			return
		}
		_, _ = w.Write([]byte(p.Subject))
	})
}

func request(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestOptional(t *testing.T) {
	tests := []struct {
		name  string
		token string
		calls int
		want  string
	}{
		{name: "no token", token: "", calls: 0, want: ""},
		{name: "valid token", token: "good", calls: 1, want: "u-1"},
		{name: "invalid token", token: "bad", calls: 1, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			v := verifier()
			rec := httptest.NewRecorder()
			auth.Optional(v)(subject()).ServeHTTP(rec, request(tt.token))
			is.Equal(rec.Code, http.StatusOK)        // never rejects
			is.Equal(len(v.VerifyCalls()), tt.calls) // verifies only a bearer token
			is.Equal(rec.Body.String(), tt.want)     // the Principal of a valid token
		})
	}
}

func TestRequireAuthenticated(t *testing.T) {
	h := auth.Optional(verifier())(auth.RequireAuthenticated()(subject()))

	t.Run("no principal", func(t *testing.T) {
		is := is.New(t)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request("bad"))
		is.Equal(rec.Code, http.StatusUnauthorized)
		var body map[string]any
		is.NoErr(json.Unmarshal(rec.Body.Bytes(), &body))
		is.Equal(body["code"], "unauthenticated")
	})

	t.Run("principal", func(t *testing.T) {
		is := is.New(t)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request("good"))
		is.Equal(rec.Code, http.StatusOK)
		is.Equal(rec.Body.String(), "u-1")
	})
}
