package prom

import (
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel/trace"
)

func newCounter() prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{Name: "test_total", Help: "h"})
}

func TestMustRegisterReturnsExistingOnDuplicate(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()

	c1 := MustRegister(reg, newCounter())
	c2 := MustRegister(reg, newCounter()) // identical name → reused, no panic

	is.Equal(c1, c2) // both calls return the registered collector
}

func TestMustRegisterPanicsOnConflict(t *testing.T) {
	reg := prometheus.NewRegistry()
	MustRegister(reg, newCounter())

	defer func() {
		if recover() == nil {
			t.Error(`MustRegister(reg, gauge "test_total") after a counter of that name did not panic, want panic`)
		}
	}()
	// Same name, different type (gauge vs counter) → genuine conflict.
	MustRegister(reg, prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_total", Help: "h"}))
}

func TestMustRegisterNilRegistryIsNoop(t *testing.T) {
	is := is.New(t)
	c := newCounter()

	is.Equal(MustRegister(nil, c), c) // returned unchanged
	c.Inc()                           // and safe to use
}

func TestObserveDurationExemplar(t *testing.T) {
	traceID := trace.TraceID{1}
	spanID := trace.SpanID{2}
	tests := []struct {
		name  string
		flags trace.TraceFlags
		want  bool
	}{
		{name: "sampled", flags: trace.FlagsSampled, want: true},
		{name: "not sampled", flags: 0, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			h := prometheus.NewHistogram(prometheus.HistogramOpts{
				Name: "test_seconds", Help: "h", Buckets: DurationBuckets(),
			})
			sc := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: traceID, SpanID: spanID, TraceFlags: tt.flags,
			})
			ctx := trace.ContextWithSpanContext(t.Context(), sc)

			ObserveDuration(ctx, h, 30*time.Millisecond)

			var m dto.Metric
			is.NoErr(h.Write(&m))
			is.Equal(m.GetHistogram().GetSampleSum(), 0.03) // seconds
			ex := bucketExemplar(m.GetHistogram())
			is.Equal(ex != nil, tt.want)
			if tt.want {
				is.Equal(label(ex, "trace_id"), traceID.String())
				is.Equal(label(ex, "span_id"), spanID.String())
			}
		})
	}
}

func TestObserveDurationWithoutSpan(t *testing.T) {
	is := is.New(t)
	h := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "test_seconds", Help: "h"})

	ObserveDuration(t.Context(), h, time.Second)

	var m dto.Metric
	is.NoErr(h.Write(&m))
	is.Equal(m.GetHistogram().GetSampleCount(), uint64(1))
	is.Equal(bucketExemplar(m.GetHistogram()), nil)
}

func bucketExemplar(h *dto.Histogram) *dto.Exemplar {
	for _, b := range h.GetBucket() {
		if ex := b.GetExemplar(); ex != nil {
			return ex
		}
	}
	return nil
}

func label(ex *dto.Exemplar, name string) string {
	for _, l := range ex.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
