// Package auth provides authentication and authorization building blocks: a
// transport-neutral Principal carried in context, credential extraction from
// HTTP requests, a pluggable Verifier (token -> Principal), a built-in JWT
// verifier, and net/http middleware for authentication and role-based access
// control.
//
// The Verifier seam keeps the middleware independent of the token format: the
// bundled JWTVerifier covers HMAC and RSA/ECDSA (and JWKS via a custom
// Keyfunc), but any Verifier — an opaque-token introspection client, a test
// stub — plugs in the same way. Rejections are written as *errs.Error JSON with
// the right status (401/403), each carrying an i18n MessageID.
//
// # Usage
//
// Build a Verifier, then compose the middleware onto your router. Authenticate
// both verifies and requires a token; RequireRole gates by role:
//
//	v, err := auth.NewJWTVerifier(auth.JWTConfig{
//	    HMACSecret: []byte(secret),
//	    Issuer:     "myapp",
//	    Audience:   "myapp-api",
//	})
//	if err != nil {
//	    return err
//	}
//
//	mux.Handle("/me", auth.Authenticate(v, log)(meHandler))
//	mux.Handle("/admin",
//	    auth.Authenticate(v, log)(auth.RequireRole("admin")(adminHandler)))
//
//	// Inside a handler, read the verified identity:
//	func meHandler(w http.ResponseWriter, r *http.Request) {
//	    p, ok := auth.PrincipalFromContext(r.Context())
//	    if !ok {
//	        return
//	    }
//	    _ = p.Subject
//	    if p.HasRole("admin") { /* ... */ }
//	}
//
// # Principal
//
// Principal is the authenticated identity (Subject, Roles, and the full verified
// Claims). HasRole / HasAnyRole answer authorization questions; HasAnyRole with
// no arguments returns true (authentication alone suffices). WithPrincipal and
// PrincipalFromContext move it through the request context.
//
// # Credential extraction
//
// BearerToken pulls a token from "Authorization: Bearer <token>"
// (case-insensitive scheme); APIKey reads a named header such as "X-API-Key".
//
// # Middleware
//
//   - Authenticate — extract, verify, and require a token (401 on failure).
//   - Optional — verify when present, never reject; an invalid token is ignored.
//   - RequireAuthenticated — reject requests with no Principal (place after
//     Optional).
//   - RequireRole — 401 when unauthenticated, 403 when authenticated without a
//     required role.
//
// # JWTConfig
//
// Exactly one key source must be set: HMACSecret, RSAPublicKeyPEM, ECPublicKeyPEM,
// or a custom Keyfunc (use the latter for JWKS). Other fields:
//
//   - ValidMethods — accepted "alg" values; defaults derive from the key source
//     (HS*/RS*/ES*) and are required when using Keyfunc.
//   - Issuer / Audience — validated against the token claims when set.
//   - Leeway — clock-skew tolerance for time-based claims.
//   - SubjectClaim — principal-id claim (default "sub").
//   - RolesClaim — roles claim (default "roles"); accepts a JSON array of
//     strings or a single space-separated string.
//
// NewJWTVerifier always requires expiration and restricts accepted algorithms,
// so "alg":"none" and algorithm-confusion attacks are rejected.
package auth
