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

// run drives a middleware-wrapped handler that returns the given ResponseEncoder.
func run(mw rest.MidFunc, resp rest.ResponseEncoder) rest.ResponseEncoder {
	h := mw(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return resp
	})
	return h(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// TestMaskInternalMasksAndLogs verifies that with mask=true a 5xx error is
// replaced by a detail-free generic error (same code) while the original detail
// is logged server-side, not leaked to the client.
func TestMaskInternalMasksAndLogs(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Service: "test", Level: logger.LevelInfo})

	orig := errs.Newf(errs.Internal, "create: pq: duplicate key %q", "secret-detail")
	got := run(mid.MaskInternal(log, true), orig)

	masked, ok := got.(*errs.Error)
	is.True(ok)                                                 // still an *errs.Error
	is.Equal(masked.Code, errs.Internal)                        // code (and thus 500 status) preserved
	is.Equal(masked.Message, "internal server error")           // detail replaced
	is.True(!strings.Contains(masked.Message, "secret-detail")) // no leak to client

	logged := buf.String()
	is.True(strings.Contains(logged, "secret-detail"))         // original detail logged
	is.True(strings.Contains(logged, "internal error masked")) // under the masking message
}

// TestMaskInternalDevPassthrough verifies that with mask=false the original error
// is returned unchanged (development).
func TestMaskInternalDevPassthrough(t *testing.T) {
	is := is.New(t)

	orig := errs.Newf(errs.Internal, "boom")
	got := run(mid.MaskInternal(nil, false), orig)
	is.Equal(got, orig) // mask=false -> unchanged
}

// TestMaskInternalLeaves4xx verifies client-actionable 4xx errors are never
// masked, even with mask=true.
func TestMaskInternalLeaves4xx(t *testing.T) {
	is := is.New(t)

	orig := errs.Newf(errs.NotFound, "widget 123 not found")
	got := run(mid.MaskInternal(nil, true), orig)
	is.Equal(got, orig) // 4xx passes through untouched
}

// TestMaskInternalPassThroughNonError verifies a success response is untouched.
func TestMaskInternalPassThroughNonError(t *testing.T) {
	is := is.New(t)

	ok := rest.JSON("ok")
	got := run(mid.MaskInternal(nil, true), ok)
	_, isErr := got.(*errs.Error)
	is.True(!isErr) // non-error stays non-error
}
