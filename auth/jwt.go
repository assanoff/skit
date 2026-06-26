package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// JWTConfig configures the built-in JWT verifier. Exactly one key source must be
// set: HMACSecret, RSAPublicKeyPEM, ECPublicKeyPEM, or a custom Keyfunc (use the
// latter for JWKS — e.g. a keyfunc backed by a remote key set).
type JWTConfig struct {
	HMACSecret      []byte
	RSAPublicKeyPEM []byte
	ECPublicKeyPEM  []byte
	// Keyfunc overrides the key sources above (e.g. JWKS). When set, ValidMethods
	// should list the algorithms you accept.
	Keyfunc jwt.Keyfunc

	// ValidMethods restricts accepted "alg" values. Defaults are derived from the
	// configured key source (HS*/RS*/ES*); set explicitly when using Keyfunc.
	ValidMethods []string

	// Issuer / Audience, when set, are validated against the token claims.
	Issuer   string
	Audience string
	// Leeway tolerates small clock skew on time-based claims.
	Leeway time.Duration

	// SubjectClaim names the principal-id claim (default "sub").
	SubjectClaim string
	// RolesClaim names the roles claim (default "roles"). Accepts a JSON array of
	// strings or a single space-separated string.
	RolesClaim string
}

// JWTVerifier verifies JWTs and maps their claims to a Principal.
type JWTVerifier struct {
	keyfunc      jwt.Keyfunc
	opts         []jwt.ParserOption
	subjectClaim string
	rolesClaim   string
}

var _ Verifier = (*JWTVerifier)(nil)

// NewJWTVerifier builds a JWTVerifier from cfg.
func NewJWTVerifier(cfg JWTConfig) (*JWTVerifier, error) {
	keyfunc, methods, err := resolveKey(cfg)
	if err != nil {
		return nil, err
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods(methods),
		jwt.WithExpirationRequired(),
	}
	if cfg.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(cfg.Issuer))
	}
	if cfg.Audience != "" {
		opts = append(opts, jwt.WithAudience(cfg.Audience))
	}
	if cfg.Leeway > 0 {
		opts = append(opts, jwt.WithLeeway(cfg.Leeway))
	}

	v := &JWTVerifier{
		keyfunc:      keyfunc,
		opts:         opts,
		subjectClaim: orDefault(cfg.SubjectClaim, "sub"),
		rolesClaim:   orDefault(cfg.RolesClaim, "roles"),
	}
	return v, nil
}

// resolveKey returns the keyfunc and the accepted algorithms for cfg.
func resolveKey(cfg JWTConfig) (jwt.Keyfunc, []string, error) {
	switch {
	case cfg.Keyfunc != nil:
		methods := cfg.ValidMethods
		if len(methods) == 0 {
			return nil, nil, fmt.Errorf("auth: jwt: ValidMethods is required with a custom Keyfunc")
		}
		return cfg.Keyfunc, methods, nil

	case len(cfg.HMACSecret) > 0:
		secret := cfg.HMACSecret
		return func(*jwt.Token) (any, error) { return secret, nil },
			methodsOr(cfg.ValidMethods, "HS256", "HS384", "HS512"), nil

	case len(cfg.RSAPublicKeyPEM) > 0:
		pub, err := jwt.ParseRSAPublicKeyFromPEM(cfg.RSAPublicKeyPEM)
		if err != nil {
			return nil, nil, fmt.Errorf("auth: jwt: parse RSA public key: %w", err)
		}
		return func(*jwt.Token) (any, error) { return pub, nil },
			methodsOr(cfg.ValidMethods, "RS256", "RS384", "RS512"), nil

	case len(cfg.ECPublicKeyPEM) > 0:
		pub, err := jwt.ParseECPublicKeyFromPEM(cfg.ECPublicKeyPEM)
		if err != nil {
			return nil, nil, fmt.Errorf("auth: jwt: parse EC public key: %w", err)
		}
		return func(*jwt.Token) (any, error) { return pub, nil },
			methodsOr(cfg.ValidMethods, "ES256", "ES384", "ES512"), nil

	default:
		return nil, nil, fmt.Errorf("auth: jwt: no key configured")
	}
}

// Verify parses and validates token, returning its Principal.
func (v *JWTVerifier) Verify(_ context.Context, token string) (*Principal, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, v.keyfunc, v.opts...)
	if err != nil {
		return nil, fmt.Errorf("auth: jwt: %w", err)
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("auth: jwt: token is invalid")
	}

	subject, _ := claims[v.subjectClaim].(string)
	return &Principal{
		Subject: subject,
		Roles:   coerceRoles(claims[v.rolesClaim]),
		Claims:  map[string]any(claims),
	}, nil
}

// coerceRoles normalizes a roles claim that may be a []any of strings or a
// single space-separated string into a []string.
func coerceRoles(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		return strings.Fields(t)
	default:
		return nil
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func methodsOr(override []string, defaults ...string) []string {
	if len(override) > 0 {
		return override
	}
	return defaults
}
