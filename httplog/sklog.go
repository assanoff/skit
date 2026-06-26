package httplog

import (
	"net/http"
	"strings"

	"github.com/assanoff/skit/logger"
)

// Middleware builds the request-logging middleware driven by a skit
// logger.Logger. It writes through that logger's handler, so HTTP access logs
// share the same output, level, and multi-sink fan-out (stdout + Sentry, ...) as
// the rest of the application — one handler, distinct instances.
//
// Pass a dedicated instance, e.g. log.Named("access"), to tag access lines.
// A nil Options falls back to the package defaults.
func Middleware(log *logger.Logger, o *Options) func(http.Handler) http.Handler {
	return RequestLogger(log.Slog(), o)
}

// clientIP resolves the client address for the request log, preferring the
// first X-Forwarded-For hop, then X-Real-IP, then RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	return r.RemoteAddr
}
