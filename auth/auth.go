package auth

import (
	"context"
	"net/http"
	"slices"
	"strings"
)

// Principal is the authenticated identity attached to a request context.
type Principal struct {
	// Subject is the unique principal id (JWT "sub").
	Subject string
	// Roles are the principal's authorization roles.
	Roles []string
	// Claims carries the full, verified claim set for application use.
	Claims map[string]any
}

// HasRole reports whether the principal has the given role.
func (p *Principal) HasRole(role string) bool {
	return slices.Contains(p.Roles, role)
}

// HasAnyRole reports whether the principal has at least one of roles. With no
// roles given it returns true (authentication alone suffices).
func (p *Principal) HasAnyRole(roles ...string) bool {
	if len(roles) == 0 {
		return true
	}
	return slices.ContainsFunc(roles, p.HasRole)
}

// Verifier turns a raw credential (e.g. a bearer token) into a Principal.
// Implementations must return a non-nil error for any invalid credential.
type Verifier interface {
	Verify(ctx context.Context, token string) (*Principal, error)
}

type ctxKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFromContext returns the principal stored by the middleware, if any.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok
}

// BearerToken extracts a token from an "Authorization: Bearer <token>" header.
// The scheme match is case-insensitive.
func BearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	return token, token != ""
}

// APIKey extracts a token from the named header (e.g. "X-API-Key").
func APIKey(r *http.Request, header string) (string, bool) {
	v := strings.TrimSpace(r.Header.Get(header))
	return v, v != ""
}
