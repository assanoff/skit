package mid_test

import (
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/mid"
)

// TestMetricsReportsCodes verifies the outcome code reported to record: the
// errs code for an error, "ok" for a success.
func TestMetricsReportsCodes(t *testing.T) {
	is := is.New(t)

	var got []string
	mw := mid.Metrics(func(code string) {
		got = append(got, code)
	})

	run(mw, errs.Newf(errs.NotFound, "missing"))
	run(mw, rest.JSON("ok"))

	is.Equal(got, []string{"not_found", "ok"}) // error code, then ok
}
