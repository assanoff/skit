package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSConfig configures the CORS middleware. The zero value allows no origins
// (every cross-origin request is passed through without CORS headers, so the
// browser blocks it) — set AllowedOrigins to opt in.
type CORSConfig struct {
	// AllowedOrigins is the set of permitted Origin values, e.g.
	// {"https://app.example.com"}. The single entry "*" allows any origin.
	AllowedOrigins []string
	// AllowedMethods defaults to GET, POST, PUT, PATCH, DELETE, OPTIONS.
	AllowedMethods []string
	// AllowedHeaders defaults to Origin, Content-Type, Accept, Authorization.
	AllowedHeaders []string
	// ExposedHeaders lists response headers the browser is allowed to read.
	ExposedHeaders []string
	// AllowCredentials sets Access-Control-Allow-Credentials. With credentials
	// the wildcard is never sent: the specific request Origin is echoed instead
	// (browsers reject "*" with credentials).
	AllowCredentials bool
	// MaxAge is the preflight cache lifetime in seconds (Access-Control-Max-Age).
	MaxAge int
}

// CORS returns middleware applying the policy in cfg. It sets the
// Access-Control-* headers on allowed cross-origin responses and answers the
// preflight OPTIONS request with 204. Requests without an Origin are passed
// through untouched; a request from a disallowed origin gets no CORS headers
// (so the browser blocks it), and a disallowed preflight is short-circuited
// with 204 and no Allow-* headers.
func CORS(cfg CORSConfig) Middleware {
	methods := cfg.AllowedMethods
	if len(methods) == 0 {
		methods = []string{
			http.MethodGet, http.MethodPost, http.MethodPut,
			http.MethodPatch, http.MethodDelete, http.MethodOptions,
		}
	}
	headers := cfg.AllowedHeaders
	if len(headers) == 0 {
		headers = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	}

	allowMethods := strings.Join(methods, ", ")
	allowHeaders := strings.Join(headers, ", ")
	exposeHeaders := strings.Join(cfg.ExposedHeaders, ", ")
	wildcard := len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			isPreflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""

			if origin == "" || !corsOriginAllowed(cfg.AllowedOrigins, origin) {
				if isPreflight {
					w.WriteHeader(http.StatusNoContent) // disallowed preflight: no Allow-* headers
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Add("Vary", "Origin")
			if wildcard && !cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			if cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if exposeHeaders != "" {
				h.Set("Access-Control-Expose-Headers", exposeHeaders)
			}

			if isPreflight {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				h.Set("Access-Control-Allow-Methods", allowMethods)
				h.Set("Access-Control-Allow-Headers", allowHeaders)
				if cfg.MaxAge > 0 {
					h.Set("Access-Control-Max-Age", strconv.Itoa(cfg.MaxAge))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func corsOriginAllowed(allowed []string, origin string) bool {
	for _, a := range allowed {
		if a == "*" || strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}
