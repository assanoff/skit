package mid_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/mid"
)

// jsonHandler is a typed handler returning v as JSON.
func jsonHandler(v any) rest.HandlerFunc {
	return func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON(v)
	}
}

// errHandler is a typed handler returning a NotFound error.
func errHandler() rest.HandlerFunc {
	return func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return errs.Newf(errs.NotFound, "nope")
	}
}

// serve drives h through the ServeHTTP boundary (which installs the writer in
// context) and returns the recorder.
func serve(h rest.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestCacheControlSetsHeaders verifies a successful response gets Cache-Control
// and Vary headers.
func TestCacheControlSetsHeaders(t *testing.T) {
	is := is.New(t)

	h := rest.ChainMiddleware(jsonHandler("ok"), mid.CacheControl(60, "Accept-Language"))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))

	is.Equal(rec.Code, http.StatusOK)
	is.Equal(rec.Header().Get("Cache-Control"), "public, max-age=60")
	is.Equal(rec.Header().Get("Vary"), "Accept-Language")
}

// TestCacheControlSkipsErrors verifies error responses are not cached.
func TestCacheControlSkipsErrors(t *testing.T) {
	is := is.New(t)

	h := rest.ChainMiddleware(errHandler(), mid.CacheControl(60))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))

	is.Equal(rec.Code, http.StatusNotFound)
	is.Equal(rec.Header().Get("Cache-Control"), "") // errors are not cached
}

// TestCacheControlNoWriter verifies a no-op when invoked off the ServeHTTP
// boundary (no writer in context).
func TestCacheControlNoWriter(t *testing.T) {
	is := is.New(t)

	h := rest.ChainMiddleware(jsonHandler("ok"), mid.CacheControl(60))
	resp := h(context.Background(), httptest.NewRequest(http.MethodGet, "/x", nil))
	is.True(resp != nil) // passes through without panicking
}
