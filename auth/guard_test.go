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

// TestGuardNoRolesIsAuthOnly verifies Guard(v) with no roles is an
// authentication-only guard: any valid token passes, regardless of roles.
func TestGuardNoRolesIsAuthOnly(t *testing.T) {
	is := is.New(t)

	h := Guard(testVerifier(t))(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON("ok")
	})

	// A valid token with no matching roles still passes (auth-only).
	tok := signHMAC(t, jwt.MapClaims{
		"sub": "u", "iss": "test-iss", "roles": []any{"viewer"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	_, isErr := h(context.Background(), req).(*errs.Error)
	is.True(!isErr) // authenticated -> allowed

	// No token -> still rejected.
	_, isErr = h(context.Background(), httptest.NewRequest(http.MethodPost, "/x", nil)).(*errs.Error)
	is.True(isErr) // unauthenticated -> rejected
}

// TestRequireRoleAppNoRolesDenies verifies RequireRoleApp with no roles denies an
// authenticated principal (deny-by-default), unlike Guard which routes around it.
func TestRequireRoleAppNoRolesDenies(t *testing.T) {
	is := is.New(t)

	h := RequireRoleApp()(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON("ok")
	})

	ctx := WithPrincipal(context.Background(), &Principal{Subject: "u", Roles: []string{"admin"}})
	e, isErr := h(ctx, httptest.NewRequest(http.MethodPost, "/x", nil)).(*errs.Error)
	is.True(isErr)                          // authenticated but no roles configured
	is.Equal(e.Code, errs.PermissionDenied) // denied by default
}
