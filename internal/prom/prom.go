// Package prom holds the Prometheus helpers the SDK packages share:
// register-or-get, the bucket bounds of durations, the exemplar of a duration
// and the metrics of the runs of periodic work.
package prom

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

// MustRegister registers c on reg and returns it. If an identical collector is
// already registered (same fully-qualified name and labels) it returns the
// existing one instead, so a package that builds its collectors in every
// constructor call can be built twice on one registry. A different collector
// under the same name panics, as prometheus.MustRegister does: constructors
// run while the service is assembled, so the wiring bug stops the start.
//
// A nil reg disables registration: c is returned unregistered and its methods
// stay safe to call.
func MustRegister[C prometheus.Collector](reg prometheus.Registerer, c C) C {
	if reg == nil {
		return c
	}
	if err := reg.Register(c); err != nil {
		if already, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
			if existing, ok := already.ExistingCollector.(C); ok {
				return existing
			}
		}
		panic(err)
	}
	return c
}

// DurationBuckets returns the bucket bounds, in seconds, of the request and run
// durations the SDK records: the bounds the OpenTelemetry semantic conventions
// give for HTTP and RPC durations.
func DurationBuckets() []float64 {
	return []float64{.005, .01, .025, .05, .075, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10}
}

// ObserveDuration records d in seconds on o. When the span of ctx is sampled,
// its trace and span ids go with the value as the exemplar, so a dashboard
// links the bucket to the trace.
func ObserveDuration(ctx context.Context, o prometheus.Observer, d time.Duration) {
	v := d.Seconds()
	sc := trace.SpanContextFromContext(ctx)
	eo, ok := o.(prometheus.ExemplarObserver)
	if !ok || !sc.IsSampled() {
		o.Observe(v)
		return
	}
	eo.ObserveWithExemplar(v, prometheus.Labels{
		"trace_id": sc.TraceID().String(),
		"span_id":  sc.SpanID().String(),
	})
}
