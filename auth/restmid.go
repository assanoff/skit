package auth

import (
	"context"
	"net/http"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
)

// AuthenticateApp is the rest.MidFunc form of Authenticate: it extracts and
// verifies the bearer token and stores the resulting Principal in the context,
// then calls the next handler. Unlike Authenticate it does not write to the
// ResponseWriter; it returns an *errs.Error (Unauthenticated) so the failure
// flows through the typed handler chain and is encoded by rest.Respond. Use it
// as a per-route guard via the rest.Handle seam:
//
//	handle("POST /widgets", h.create, auth.AuthenticateApp(verifier))
func AuthenticateApp(v Verifier) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			token, ok := BearerToken(r)
			if !ok {
				return errs.Newf(errs.Unauthenticated, "missing bearer token").
					WithMessageID("auth.missing_token")
			}
			p, err := v.Verify(ctx, token)
			if err != nil {
				return errs.Wrapf(errs.Unauthenticated, err, "invalid token").
					WithMessageID("auth.invalid_token")
			}
			return next(WithPrincipal(ctx, p), r)
		}
	}
}

// OptionalApp is the rest.MidFunc form of Optional: it stores the Principal when
// a valid bearer token is present but never rejects the request. Pair it with
// RequireAuthenticatedApp or RequireRoleApp to enforce access per route.
func OptionalApp(v Verifier) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			if token, ok := BearerToken(r); ok {
				if p, err := v.Verify(ctx, token); err == nil {
					ctx = WithPrincipal(ctx, p)
				}
			}
			return next(ctx, r)
		}
	}
}

// RequireAuthenticatedApp is the rest.MidFunc form of RequireAuthenticated: it
// rejects requests that carry no Principal with Unauthenticated. Place it after
// OptionalApp, or use AuthenticateApp which both verifies and requires.
func RequireAuthenticatedApp() rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			if _, ok := PrincipalFromContext(ctx); !ok {
				return errs.Newf(errs.Unauthenticated, "authentication required").
					WithMessageID("auth.required")
			}
			return next(ctx, r)
		}
	}
}

// RequireRoleApp is the rest.MidFunc form of RequireRole: it rejects requests
// whose Principal lacks at least one of roles — Unauthenticated when no
// principal is present, PermissionDenied when one is present without a required
// role. With no roles given, authentication alone suffices.
func RequireRoleApp(roles ...string) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			p, ok := PrincipalFromContext(ctx)
			if !ok {
				return errs.Newf(errs.Unauthenticated, "authentication required").
					WithMessageID("auth.required")
			}
			if !p.HasAnyRole(roles...) {
				return errs.Newf(errs.PermissionDenied, "insufficient permissions").
					WithMessageID("auth.forbidden")
			}
			return next(ctx, r)
		}
	}
}

// Guard is the common case sealed into one rest.MidFunc: it composes
// AuthenticateApp and RequireRoleApp (via rest.Chain) so a single value verifies
// the bearer token, stores the Principal, and requires at least one of roles
// (authentication alone when no roles are given).
//
// It returns nil when v is nil — a no-op that the rest.Handle seam and
// ChainMiddleware skip — so a caller can build it unconditionally and stay public
// when auth is disabled (the verifier is nil). Build it where you have the
// verifier and apply it at any level: a single route via the Handle seam, a group
// via router.WithApp, or the whole router via UseApp.
//
//	guard := auth.Guard(verifier, "admin") // nil (public) when verifier is nil
//	handle("POST /widgets", h.create, guard)
func Guard(v Verifier, roles ...string) rest.MidFunc {
	if v == nil {
		return nil
	}
	return rest.Chain(AuthenticateApp(v), RequireRoleApp(roles...))
}
