package grpcgateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/matryer/is"
)

// route renders the pattern the mux matched as the template of its rule; a
// variable bound to a longer path keeps its binding.
func TestRoute(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template string
		path     string
	}{
		{name: "variable", template: "/v1/widgets/{id}", path: "/v1/widgets/42"},
		{name: "bound", template: "/v1/{name=shelves/*}", path: "/v1/shelves/7"},
		{name: "verb", template: "/v1/widgets/{id}:archive", path: "/v1/widgets/42:archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			is := is.New(t)
			var got string
			mux := runtime.NewServeMux()
			err := mux.HandlePath(http.MethodPost, tc.template,
				func(_ http.ResponseWriter, r *http.Request, _ map[string]string) {
					p, ok := runtime.HTTPPattern(r.Context())
					is.True(ok)
					got = route(p)
				})
			is.NoErr(err)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, tc.path, nil)
			mux.ServeHTTP(httptest.NewRecorder(), req)

			is.Equal(got, tc.template)
		})
	}
}
