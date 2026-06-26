package mid_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/mid"
)

// TestPanicsRecoversToInternal verifies a panic becomes a detail-free Internal
// *errs.Error (so the pipeline can encode/mask/localize it) and the stack is
// logged, not leaked.
func TestPanicsRecoversToInternal(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Service: "test", Level: logger.LevelInfo})

	h := mid.Panics(log)(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		panic("boom-secret")
	})
	resp := h(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))

	e, ok := resp.(*errs.Error)
	is.True(ok)                                   // panic became an *errs.Error
	is.Equal(e.Code, errs.Internal)               // of code Internal
	is.True(!strings.Contains(e.Message, "boom")) // panic value not in the client detail

	logged := buf.String()
	is.True(strings.Contains(logged, "panic recovered")) // logged
	is.True(strings.Contains(logged, "boom-secret"))     // with the panic value
}

// TestPanicsPassThrough verifies a non-panicking handler is untouched.
func TestPanicsPassThrough(t *testing.T) {
	is := is.New(t)

	h := mid.Panics(nil)(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return rest.JSON("ok")
	})
	resp := h(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	_, isErr := resp.(*errs.Error)
	is.True(!isErr) // no panic -> no error
}
