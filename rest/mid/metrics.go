package mid

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/internal/prom"
	"github.com/assanoff/skit/rest"
)

// newOutcomes builds skit_rest_outcomes_total and registers it on reg. A
// second Chain on the same reg shares it.
func newOutcomes(reg prometheus.Registerer) *prometheus.CounterVec {
	return prom.MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "skit", Subsystem: "rest",
		Name: "outcomes_total",
		Help: "Requests by outcome: the errs code of an error response, else ok.",
	}, []string{"code"}))
}

// metrics counts the outcome of each request in outcomes, by the domain code
// rather than the HTTP status.
func metrics(outcomes *prometheus.CounterVec) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			resp := next(ctx, r)
			outcomes.WithLabelValues(outcome(resp)).Inc()
			return resp
		}
	}
}

// outcome is the code of an *errs.Error response ("not_found", "internal"),
// else "ok".
func outcome(resp rest.ResponseEncoder) string {
	if e, ok := resp.(*errs.Error); ok {
		return e.CodeStr
	}
	return "ok"
}
