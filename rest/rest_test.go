package rest_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/rest"
)

// TestHandlerFuncServeHTTP verifies the boundary: a typed HandlerFunc is an
// http.Handler, so it registers directly on a net/http mux and its ResponseEncoder is
// written by Respond.
func TestHandlerFuncServeHTTP(t *testing.T) {
	is := is.New(t)

	mux := http.NewServeMux()
	mux.Handle("GET /widget", rest.HandlerFunc(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON(map[string]string{"name": "gadget"})
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/widget")
	is.NoErr(err) // GET /widget
	defer resp.Body.Close()

	is.Equal(resp.StatusCode, http.StatusOK)                      // status 200
	is.Equal(resp.Header.Get("Content-Type"), "application/json") // content type

	body, err := io.ReadAll(resp.Body)
	is.NoErr(err)
	is.Equal(string(body), `{"name":"gadget"}`) // encoded body
}

// TestHandlerFuncServeHTTPNil verifies a nil ResponseEncoder yields 204 through the
// boundary.
func TestHandlerFuncServeHTTPNil(t *testing.T) {
	is := is.New(t)

	h := rest.HandlerFunc(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return nil
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/x", nil))
	is.Equal(rec.Code, http.StatusNoContent) // nil ResponseEncoder -> 204
}

// TestChainMiddlewareIsHTTPHandler verifies a chained typed handler is still an
// http.Handler (ChainMiddleware returns a HandlerFunc), and that mw[0] is the
// outermost layer.
func TestChainMiddlewareIsHTTPHandler(t *testing.T) {
	is := is.New(t)

	var order []string
	mark := func(name string) rest.MidFunc {
		return func(next rest.HandlerFunc) rest.HandlerFunc {
			return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
				order = append(order, name)
				return next(ctx, r)
			}
		}
	}
	h := rest.ChainMiddleware(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		order = append(order, "handler")
		return rest.JSON("ok")
	}, mark("outer"), mark("inner"))

	var hh http.Handler = h // compile-time: HandlerFunc satisfies http.Handler
	hh.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	is.Equal(order, []string{"outer", "inner", "handler"}) // mw[0] is outermost
}

// mark returns a MidFunc that appends name to *order before delegating, so a
// test can assert the wrapping order of a chain.
func mark(order *[]string, name string) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			*order = append(*order, name)
			return next(ctx, r)
		}
	}
}

// TestChainOrderAndNesting verifies Chain collapses several MidFunc into one
// applied outermost-first, and that a sealed Chain is itself a first-class
// MidFunc that composes inside another chain.
func TestChainOrderAndNesting(t *testing.T) {
	is := is.New(t)

	var order []string
	inner := rest.Chain(mark(&order, "b"), mark(&order, "c")) // a reusable bundle as one value

	// Compose the bundle inside another chain: a wraps the (b, c) bundle wraps d.
	h := rest.ChainMiddleware(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		order = append(order, "handler")
		return rest.JSON("ok")
	}, mark(&order, "a"), inner, mark(&order, "d"))

	h(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))

	is.Equal(order, []string{"a", "b", "c", "d", "handler"}) // outermost-first, bundle preserves order
}

// TestChainIdentity verifies that an empty Chain — and one of only nil
// elements — is a no-op: it returns the handler's response untouched.
func TestChainIdentity(t *testing.T) {
	is := is.New(t)

	handler := func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON("ok")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	empty := rest.Chain()(handler)
	data, _, err := empty(context.Background(), req).Encode()
	is.NoErr(err)
	is.Equal(string(data), `"ok"`) // empty Chain passes the response through

	allNil := rest.Chain(nil, nil)(handler)
	data, _, err = allNil(context.Background(), req).Encode()
	is.NoErr(err)
	is.Equal(string(data), `"ok"`) // nil elements are skipped
}
