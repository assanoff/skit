package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

var testSecret = []byte("super-secret-key")

func signHMAC(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(testSecret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func testVerifier(t *testing.T) *JWTVerifier {
	t.Helper()
	v, err := NewJWTVerifier(JWTConfig{HMACSecret: testSecret, Issuer: "test-iss"})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	return v
}

func TestJWTVerifyValid(t *testing.T) {
	v := testVerifier(t)
	token := signHMAC(t, jwt.MapClaims{
		"sub":   "user-1",
		"iss":   "test-iss",
		"roles": []any{"admin", "editor"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	p, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if p.Subject != "user-1" {
		t.Errorf("subject = %q", p.Subject)
	}
	if !p.HasRole("admin") || !p.HasRole("editor") {
		t.Errorf("roles = %v", p.Roles)
	}
	if p.Claims["iss"] != "test-iss" {
		t.Errorf("claims missing iss: %v", p.Claims)
	}
}

func TestJWTVerifyRejects(t *testing.T) {
	v := testVerifier(t)

	t.Run("expired", func(t *testing.T) {
		token := signHMAC(t, jwt.MapClaims{"sub": "u", "iss": "test-iss", "exp": time.Now().Add(-time.Hour).Unix()})
		if _, err := v.Verify(context.Background(), token); err == nil {
			t.Error("expected expired token to fail")
		}
	})

	t.Run("wrong issuer", func(t *testing.T) {
		token := signHMAC(t, jwt.MapClaims{"sub": "u", "iss": "other", "exp": time.Now().Add(time.Hour).Unix()})
		if _, err := v.Verify(context.Background(), token); err == nil {
			t.Error("expected wrong-issuer token to fail")
		}
	})

	t.Run("missing exp", func(t *testing.T) {
		token := signHMAC(t, jwt.MapClaims{"sub": "u", "iss": "test-iss"})
		if _, err := v.Verify(context.Background(), token); err == nil {
			t.Error("expected token without exp to fail (expiration required)")
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u", "iss": "test-iss", "exp": time.Now().Add(time.Hour).Unix()})
		bad, _ := tok.SignedString([]byte("not-the-secret"))
		if _, err := v.Verify(context.Background(), bad); err == nil {
			t.Error("expected bad-signature token to fail")
		}
	})

	t.Run("alg none", func(t *testing.T) {
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": "u", "iss": "test-iss", "exp": time.Now().Add(time.Hour).Unix()})
		s, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if _, err := v.Verify(context.Background(), s); err == nil {
			t.Error("expected alg=none token to be rejected")
		}
	})
}

func TestNoKeyConfigured(t *testing.T) {
	if _, err := NewJWTVerifier(JWTConfig{}); err == nil {
		t.Error("expected error when no key source is configured")
	}
}

func TestBearerTokenExtraction(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer abc.def.ghi")
	if tok, ok := BearerToken(r); !ok || tok != "abc.def.ghi" {
		t.Errorf("BearerToken = %q, %v", tok, ok)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := BearerToken(r2); ok {
		t.Error("expected no token without header")
	}
}

func TestAuthenticateMiddleware(t *testing.T) {
	v := testVerifier(t)
	var seen *Principal
	protected := Authenticate(v, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := PrincipalFromContext(r.Context())
		seen = p
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("missing token -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["code"] != "unauthenticated" {
			t.Errorf("error code = %v, want unauthenticated", body["code"])
		}
	})

	t.Run("valid token -> 200", func(t *testing.T) {
		token := signHMAC(t, jwt.MapClaims{"sub": "u-9", "iss": "test-iss", "exp": time.Now().Add(time.Hour).Unix()})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if seen == nil || seen.Subject != "u-9" {
			t.Errorf("principal not in context: %+v", seen)
		}
	})
}

func TestRequireRole(t *testing.T) {
	handler := func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	}

	t.Run("unauthenticated -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RequireRole("admin")(handler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("missing role -> 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(WithPrincipal(req.Context(), &Principal{Subject: "u", Roles: []string{"viewer"}}))
		rec := httptest.NewRecorder()
		RequireRole("admin")(handler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("has role -> 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(WithPrincipal(req.Context(), &Principal{Subject: "u", Roles: []string{"admin"}}))
		rec := httptest.NewRecorder()
		RequireRole("admin", "editor")(handler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("no roles configured -> 403 (deny by default)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(WithPrincipal(req.Context(), &Principal{Subject: "u", Roles: []string{"admin"}}))
		rec := httptest.NewRecorder()
		RequireRole()(handler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}
