package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/assanoff/skit/auth"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/router"
)

var secret = []byte("an-hmac-secret-of-32-bytes-long!")

// sign issues the token an identity provider would: HS256 with the subject,
// the roles, the issuer and an expiry.
func sign(sub string, roles ...string) string {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   sub,
		"roles": roles,
		"iss":   "shop",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	s, err := tok.SignedString(secret)
	if err != nil {
		panic(err)
	}
	return s
}

// call sends method path with the bearer token, if any, to h and prints the
// status and the body.
func call(h http.Handler, method, path, token string) {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	fmt.Println(strings.TrimSpace(fmt.Sprintf("%d %s", w.Code, w.Body)))
}

// Guard verifies the bearer token and requires a role in one rest.MidFunc: no
// token is a 401, a token without the role a 403. The handler reads the
// verified identity from the context.
func Example() {
	v, err := auth.NewJWTVerifier(auth.JWTConfig{HMACSecret: secret, Issuer: "shop"})
	if err != nil {
		fmt.Println(err)
		return
	}

	r := router.New()
	r.HandleApp("DELETE /widgets/{id}", func(ctx context.Context, _ *http.Request) rest.ResponseEncoder {
		p, _ := auth.PrincipalFromContext(ctx)
		return rest.JSON(map[string]string{"deleted_by": p.Subject})
	}, auth.Guard(v, "admin"))

	call(r, http.MethodDelete, "/widgets/42", "")
	call(r, http.MethodDelete, "/widgets/42", sign("u-1", "viewer"))
	call(r, http.MethodDelete, "/widgets/42", sign("u-2", "admin"))
	// Output:
	// 401 {"code":"unauthenticated","detail":"missing bearer token"}
	// 403 {"code":"permission_denied","detail":"insufficient permissions"}
	// 200 {"deleted_by":"u-2"}
}

// Authenticate and RequireRole are the net/http forms, for a handler outside
// the typed layer.
func ExampleAuthenticate() {
	v, err := auth.NewJWTVerifier(auth.JWTConfig{HMACSecret: secret})
	if err != nil {
		fmt.Println(err)
		return
	}
	me := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFromContext(r.Context())
		fmt.Fprintln(w, p.Subject, p.Roles, p.HasRole("admin"))
	})

	mux := http.NewServeMux()
	mux.Handle("GET /me", auth.Authenticate(v, nil)(me))
	mux.Handle("GET /admin", auth.Authenticate(v, nil)(auth.RequireRole("admin")(me)))

	call(mux, http.MethodGet, "/me", "not-a-jwt")
	call(mux, http.MethodGet, "/me", sign("u-1", "viewer"))
	call(mux, http.MethodGet, "/admin", sign("u-1", "viewer"))
	// Output:
	// 401 {"code":"unauthenticated","detail":"invalid token: auth: jwt: token is malformed: token contains an invalid number of segments"}
	// 200 u-1 [viewer] false
	// 403 {"code":"permission_denied","detail":"insufficient permissions"}
}

// OptionalApp reads a token when there is one and never rejects: a public
// route serves guests and members alike. A route that needs the identity adds
// RequireAuthenticatedApp.
func ExampleOptionalApp() {
	v, err := auth.NewJWTVerifier(auth.JWTConfig{HMACSecret: secret})
	if err != nil {
		fmt.Println(err)
		return
	}
	greet := func(ctx context.Context, _ *http.Request) rest.ResponseEncoder {
		name := "guest"
		if p, ok := auth.PrincipalFromContext(ctx); ok {
			name = p.Subject
		}
		return rest.JSON(map[string]string{"hello": name})
	}

	r := router.New(auth.OptionalApp(v))
	r.HandleApp("GET /hello", greet)
	r.HandleApp("GET /orders", greet, auth.RequireAuthenticatedApp())

	call(r, http.MethodGet, "/hello", "")
	call(r, http.MethodGet, "/hello", sign("u-1"))
	call(r, http.MethodGet, "/orders", "")
	// Output:
	// 200 {"hello":"guest"}
	// 200 {"hello":"u-1"}
	// 401 {"code":"unauthenticated","detail":"authentication required"}
}

// apiKeys is a Verifier of API keys: any credential plugs in behind the
// Verifier seam, not only a JWT.
type apiKeys map[string]*auth.Principal

func (k apiKeys) Verify(_ context.Context, key string) (*auth.Principal, error) {
	p, ok := k[key]
	if !ok {
		return nil, errors.New("unknown api key")
	}
	return p, nil
}

// APIKey reads a credential from a named header; a middleware of the service
// verifies it and stores the Principal with WithPrincipal.
func ExampleAPIKey() {
	var v auth.Verifier = apiKeys{"k-123": {Subject: "partner-1", Roles: []string{"partner"}}}
	partner := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, _ := auth.APIKey(r, "X-Api-Key")
			p, err := v.Verify(r.Context(), key)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		})
	}
	h := partner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFromContext(r.Context())
		fmt.Fprintln(w, p.Subject, p.HasAnyRole("partner", "admin"))
	}))

	for _, key := range []string{"k-123", "k-000"} {
		req := httptest.NewRequest(http.MethodGet, "/feed", nil)
		req.Header.Set("X-Api-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		fmt.Println(strings.TrimSpace(fmt.Sprintf("%d %s", w.Code, w.Body)))
	}
	// Output:
	// 200 partner-1 true
	// 401 unauthorized
}
