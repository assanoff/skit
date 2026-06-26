package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/matryer/is"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
)

// TestGuardNilVerifierIsPublic verifies Guard(nil) returns a nil rest.MidFunc —
// a no-op the Handle seam and ChainMiddleware skip — so a caller can build it
// unconditionally and routes stay public when auth is disabled.
func TestGuardNilVerifierIsPublic(t *testing.T) {
	is := is.New(t)

	is.Equal(Guard(nil), nil)          // nil verifier -> nil middleware
	is.Equal(Guard(nil, "admin"), nil) // roles don't matter when the verifier is nil
}

// TestGuardEnforcesAuthAndRole verifies a built Guard rejects a missing token
// (Unauthenticated) and a valid token lacking the role (PermissionDenied), and
// passes a valid token carrying the role.
func TestGuardEnforcesAuthAndRole(t *testing.T) {
	is := is.New(t)

	guard := Guard(testVerifier(t), "admin")
	is.True(guard != nil) // built when a verifier is present

	ok := func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON("ok")
	}
	h := guard(ok)

	// No token -> Unauthenticated.
	resp := h(context.Background(), httptest.NewRequest(http.MethodPost, "/x", nil))
	e, isErr := resp.(*errs.Error)
	is.True(isErr)                         // rejected
	is.Equal(e.Code, errs.Unauthenticated) // missing token

	// Valid token without the role -> PermissionDenied.
	viewer := signHMAC(t, jwt.MapClaims{
		"sub": "u", "iss": "test-iss", "roles": []any{"viewer"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+viewer)
	resp = h(context.Background(), req)
	e, isErr = resp.(*errs.Error)
	is.True(isErr)                          // rejected
	is.Equal(e.Code, errs.PermissionDenied) // wrong role

	// Valid admin token -> passes through to the handler.
	admin := signHMAC(t, jwt.MapClaims{
		"sub": "u", "iss": "test-iss", "roles": []any{"admin"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	resp = h(context.Background(), req)
	_, isErr = resp.(*errs.Error)
	is.True(!isErr) // authorized
}
