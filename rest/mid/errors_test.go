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

// TestErrorsLogs5xx verifies a 5xx error is logged with its code and detail.
func TestErrorsLogs5xx(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Service: "test", Level: logger.LevelInfo})

	got := run(mid.Errors(log), errs.Newf(errs.Internal, "db exploded"))
	_, ok := got.(*errs.Error)
	is.True(ok) // response passes through unchanged

	logged := buf.String()
	is.True(strings.Contains(logged, "request failed")) // logged
	is.True(strings.Contains(logged, "db exploded"))    // with the detail
	is.True(strings.Contains(logged, "internal"))       // and the code
}

// TestErrorsSkips4xx verifies a 4xx error is not logged by Errors (the access
// log covers those).
func TestErrorsSkips4xx(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Service: "test", Level: logger.LevelInfo})

	got := run(mid.Errors(log), errs.Newf(errs.NotFound, "missing"))
	_, ok := got.(*errs.Error)
	is.True(ok)
	is.Equal(buf.Len(), 0) // 4xx not logged here
}

// TestErrorsPassThroughSuccess verifies a success response is untouched and
// unlogged.
func TestErrorsPassThroughSuccess(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Service: "test", Level: logger.LevelInfo})

	got := run(mid.Errors(log), rest.JSON("ok"))
	_, isErr := got.(*errs.Error)
	is.True(!isErr)
	is.Equal(buf.Len(), 0)
}

// TestErrorsChainMasksAfterLog verifies Errors logs the original detail while
// MaskInternal hides it from the client (Errors inside MaskInternal).
func TestErrorsChainMasksAfterLog(t *testing.T) {
	is := is.New(t)

	var buf bytes.Buffer
	log := logger.New(&buf, logger.Config{Service: "test", Level: logger.LevelInfo})

	// outermost..innermost: MaskInternal, Errors
	h := rest.ChainMiddleware(func(_ context.Context, _ *http.Request) rest.ResponseEncoder {
		return errs.Newf(errs.Internal, "secret detail")
	}, mid.MaskInternal(nil, true), mid.Errors(log))

	resp := h(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	e, ok := resp.(*errs.Error)
	is.True(ok)
	is.Equal(e.Message, "internal server error")             // client sees masked
	is.True(strings.Contains(buf.String(), "secret detail")) // log keeps original
}
