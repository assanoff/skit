package mid_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/mid"
)

// TestETagAndConditional verifies an ETag is set on a successful response and a
// matching If-None-Match yields 304 with no body.
func TestETagAndConditional(t *testing.T) {
	is := is.New(t)

	h := rest.ChainMiddleware(jsonHandler(map[string]string{"name": "gadget"}), mid.ETag())

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))
	is.Equal(rec.Code, http.StatusOK)
	etag := rec.Header().Get("ETag")
	is.True(etag != "")         // ETag computed from the body
	is.True(rec.Body.Len() > 0) // full body on first GET

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("If-None-Match", etag)
	rec2 := serve(h, req)
	is.Equal(rec2.Code, http.StatusNotModified) // conditional GET -> 304
	is.Equal(rec2.Body.Len(), 0)                // no body on 304
	is.Equal(rec2.Header().Get("ETag"), etag)   // tag echoed
}

// TestETagStableAcrossCalls verifies the same body yields the same tag.
func TestETagStableAcrossCalls(t *testing.T) {
	is := is.New(t)

	h := rest.ChainMiddleware(jsonHandler(map[string]int{"n": 1}), mid.ETag())
	a := serve(h, httptest.NewRequest(http.MethodGet, "/x", nil)).Header().Get("ETag")
	b := serve(h, httptest.NewRequest(http.MethodGet, "/x", nil)).Header().Get("ETag")
	is.Equal(a, b) // deterministic tag
}

// TestETagSkipsErrors verifies error responses get no ETag.
func TestETagSkipsErrors(t *testing.T) {
	is := is.New(t)

	h := rest.ChainMiddleware(errHandler(), mid.ETag())
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))
	is.Equal(rec.Header().Get("ETag"), "") // errors are not tagged
}
