package auth

import (
	"net/http"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/logger"
)

// Middleware is standard net/http middleware (composes with router.Use).
type Middleware func(http.Handler) http.Handler

// Authenticate extracts a bearer token, verifies it, and stores the resulting
// Principal in the request context. Requests without a valid token are rejected
// with 401. log may be nil.
func Authenticate(v Verifier, log *logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := BearerToken(r)
			if !ok {
				writeErr(w, r, log, errs.Newf(errs.Unauthenticated, "missing bearer token").
					WithMessageID("auth.missing_token"))
				return
			}
			p, err := v.Verify(r.Context(), token)
			if err != nil {
				writeErr(w, r, log, errs.Wrapf(errs.Unauthenticated, err, "invalid token").
					WithMessageID("auth.invalid_token"))
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

// Optional verifies a bearer token when present and stores the Principal, but
// never rejects the request. Downstream handlers use PrincipalFromContext (or
// RequireAuthenticated) to enforce access. An invalid token is ignored.
func Optional(v Verifier) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token, ok := BearerToken(r); ok {
				if p, err := v.Verify(r.Context(), token); err == nil {
					r = r.WithContext(WithPrincipal(r.Context(), p))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuthenticated rejects requests that carry no Principal with 401. Place
// it after Optional, or use Authenticate which both verifies and requires.
func RequireAuthenticated() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := PrincipalFromContext(r.Context()); !ok {
				writeErr(w, r, nil, errs.Newf(errs.Unauthenticated, "authentication required").
					WithMessageID("auth.required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole rejects requests whose Principal lacks at least one of roles:
// 401 when unauthenticated, 403 when authenticated without a required role.
func RequireRole(roles ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok {
				writeErr(w, r, nil, errs.Newf(errs.Unauthenticated, "authentication required").
					WithMessageID("auth.required"))
				return
			}
			if !p.HasAnyRole(roles...) {
				writeErr(w, r, nil, errs.Newf(errs.PermissionDenied, "insufficient permissions").
					WithMessageID("auth.forbidden"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeErr renders an *errs.Error as a JSON response with its HTTP status,
// matching the rest package's encoding.
func writeErr(w http.ResponseWriter, r *http.Request, log *logger.Logger, e *errs.Error) {
	body, contentType, encErr := e.Encode()
	if encErr != nil {
		http.Error(w, e.Message, e.HTTPStatus())
		return
	}
	if log != nil {
		log.Info(r.Context(), "auth: request rejected", "code", e.CodeStr, "detail", e.Message)
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(e.HTTPStatus())
	_, _ = w.Write(body)
}
