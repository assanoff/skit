package mid

import (
	"context"
	"net/http"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
)

// Metrics returns application middleware that reports the outcome code of each
// request to record: the *errs.Error code (e.g. "internal", "not_found") for an
// error response, or "ok" for a success. It is errs-aware (counts by domain code,
// not just HTTP status) and backend-agnostic — record wires it to whatever metric
// system the app uses (e.g. a Prometheus CounterVec labeled by code). A nil
// record is a no-op.
func Metrics(record func(code string)) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			resp := next(ctx, r)
			if record == nil {
				return resp
			}
			if e, ok := resp.(*errs.Error); ok {
				record(e.CodeStr)
			} else {
				record("ok")
			}
			return resp
		}
	}
}
