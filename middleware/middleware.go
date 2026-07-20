package middleware

import (
	"net/http"
)

// Middleware is standard net/http middleware.
type Middleware = func(http.Handler) http.Handler
