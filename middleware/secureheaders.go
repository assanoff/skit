package middleware

import "net/http"

// SecureHeaders sets a conservative set of security response headers on every
// response — safe defaults for a JSON API:
//
//	X-Content-Type-Options: nosniff   // do not MIME-sniff the body
//	X-Frame-Options: DENY             // never framed (clickjacking)
//	Referrer-Policy: no-referrer      // do not leak the URL in Referer
//
// It only sets a header when the handler has not already set it, so a handler
// that needs a different value (e.g. an endpoint meant to be embedded) wins.
func SecureHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			setIfAbsent(h, "X-Content-Type-Options", "nosniff")
			setIfAbsent(h, "X-Frame-Options", "DENY")
			setIfAbsent(h, "Referrer-Policy", "no-referrer")
			next.ServeHTTP(w, r)
		})
	}
}

func setIfAbsent(h http.Header, key, value string) {
	if h.Get(key) == "" {
		h.Set(key, value)
	}
}
