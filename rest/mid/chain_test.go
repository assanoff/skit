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

// TestChainOrder verifies Chain returns the standard app middleware, and one
// more when a metric recorder is configured.
func TestChainOrder(t *testing.T) {
	is := is.New(t)

	mids := mid.Chain(mid.Config{})
	is.Equal(len(mids), 4) // LocalizeErrors, MaskInternal, Errors, Panics

	withMetric := mid.Chain(mid.Config{RecordMetric: func(string) {}})
	is.Equal(len(withMetric), 5) // + Metrics
}

// TestChainMasksInternal verifies the assembled chain masks a 5xx error when
// MaskInternal is configured (and LocalizeErrors is a safe no-op with no
// translator).
func TestChainMasksInternal(t *testing.T) {
	is := is.New(t)

	mids := mid.Chain(mid.Config{MaskInternal: true})
	h := rest.ChainMiddleware(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return errs.Newf(errs.Internal, "secret %q", "detail")
	}, mids...)

	resp := h(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	e, ok := resp.(*errs.Error)
	is.True(ok)
	is.Equal(e.Message, "internal server error") // masked by the chain
}
