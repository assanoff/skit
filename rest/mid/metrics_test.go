package mid_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/rest"
	"github.com/assanoff/skit/rest/mid"
)

// The chain counts each request by its outcome: the errs code of an error,
// "internal" for a panic, "ok" for a success.
func TestChainCountsOutcomes(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	mids := mid.Chain(mid.Config{Metrics: reg})
	handlers := []rest.HandlerFunc{
		func(context.Context, *http.Request) rest.ResponseEncoder {
			return errs.Newf(errs.NotFound, "missing")
		},
		func(context.Context, *http.Request) rest.ResponseEncoder {
			panic("boom")
		},
		func(context.Context, *http.Request) rest.ResponseEncoder {
			return rest.JSON("ok")
		},
	}

	for _, h := range handlers {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		rest.ChainMiddleware(h, mids...)(t.Context(), r)
	}

	want := `
# HELP skit_rest_outcomes_total Requests by outcome: the errs code of an error response, else ok.
# TYPE skit_rest_outcomes_total counter
skit_rest_outcomes_total{code="internal"} 1
skit_rest_outcomes_total{code="not_found"} 1
skit_rest_outcomes_total{code="ok"} 1
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_rest_outcomes_total"))
}

// Two chains on one registry, such as two servers, share the counter instead
// of failing to register.
func TestChainSharedRegistry(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	mid.Chain(mid.Config{Metrics: reg})
	mids := mid.Chain(mid.Config{Metrics: reg})
	h := func(context.Context, *http.Request) rest.ResponseEncoder {
		return rest.JSON("ok")
	}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	rest.ChainMiddleware(h, mids...)(t.Context(), r)

	n, err := testutil.GatherAndCount(reg, "skit_rest_outcomes_total")
	is.NoErr(err)
	is.Equal(n, 1)
}
