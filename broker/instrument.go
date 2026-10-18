package broker

import (
	"cmp"
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker/internal/brokertrace"
	"github.com/assanoff/skit/internal/prom"
	"github.com/assanoff/skit/otel"
)

// The consumer metrics are named skit_broker_*.
const (
	metricsNamespace = "skit"
	metricsSubsystem = "broker"
)

// scope names the tracer of this package.
const scope = "github.com/assanoff/skit/broker"

// InstrumentConfig configures Instrument.
type InstrumentConfig struct {
	// Consumer names the consumer in the metrics (default "default").
	Consumer string
	// System is messaging.system of the consumer span: "rabbitmq", "kafka".
	System string
	// Metrics is the registry for the consumer metrics; nil — metrics off.
	Metrics prometheus.Registerer
	// TracerProvider records the consumer span of every message; nil → the
	// global provider.
	TracerProvider trace.TracerProvider
}

// Instrument wraps h so every message it handles is recorded as a span and,
// with cfg.Metrics, in the metrics:
//
//	skit_broker_consumed_total{consumer,action}   — messages by verdict
//	skit_broker_handle_duration_seconds{consumer} — duration of h
//
// action is "ack", "requeue", "discard", or "panic" when h panics; the panic
// goes on to the Guard of the consumer, which requeues the message. A verdict
// outside the three is recorded as "ack", as the adapters treat it. Wrap the
// handler you pass to an adapter or to Process, so a retried message is
// recorded on every attempt.
//
// The span is a CONSUMER span "process <topic>". The context the message was
// created in, carried in its headers, is both the parent and the link of the
// span, so a request, its event and the handling of the event are one trace
// under one sampling decision. A requeue, a discard and a panic mark the span
// failed with that error.type.
func Instrument(h Handler, cfg InstrumentConfig) Handler {
	m := newConsumerMetrics(cfg.Metrics, cmp.Or(cfg.Consumer, "default"))
	tracer := otel.Tracer(cfg.TracerProvider, scope)
	return func(ctx context.Context, msg Message) Action {
		ctx, span := startProcess(ctx, tracer, cfg.System, msg)
		start := time.Now()
		action := Requeue
		// finished stays false when h panics: the deferred record then counts a
		// panic while the panic goes on to the Guard of the consumer.
		finished := false
		defer func() {
			m.record(ctx, time.Since(start), action, finished)
			endProcess(span, action, finished)
		}()
		action = h(ctx, msg)
		finished = true
		return action
	}
}

// startProcess opens the consumer span of msg under the context msg was created
// in, linked to it as well; without one the span continues ctx.
func startProcess(ctx context.Context, tracer trace.Tracer, system string, msg Message) (context.Context, trace.Span) {
	name := "process"
	attrs := []attribute.KeyValue{
		semconv.MessagingOperationName("process"),
		semconv.MessagingOperationTypeProcess,
		semconv.MessagingMessageID(msg.ID),
	}
	if msg.Topic != "" {
		name += " " + msg.Topic
		attrs = append(attrs, semconv.MessagingDestinationName(msg.Topic))
	}
	if system != "" {
		attrs = append(attrs, semconv.MessagingSystemKey.String(system))
	}
	opts := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attrs...)}
	if created := brokertrace.Created(msg.Headers); created.IsValid() {
		ctx = trace.ContextWithRemoteSpanContext(ctx, created)
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: created}))
	}
	return tracer.Start(ctx, name, opts...) //nolint:spancheck // Instrument ends it with endProcess
}

// endProcess ends span: a handler that did not finish panicked; a requeue or a
// discard did not process the message.
func endProcess(span trace.Span, action Action, finished bool) {
	switch {
	case !finished:
		otel.SetError(span, verdictError("panic"))
	case action == Requeue:
		otel.SetError(span, verdictError("requeue"))
	case action == Discard:
		otel.SetError(span, verdictError("discard"))
	}
	span.End()
}

// verdictError is the outcome of a message the handler did not process; it is
// its own error.type.
type verdictError string

func (e verdictError) Error() string { return string(e) }

// ErrorType names the error for error.type.
func (e verdictError) ErrorType() string { return string(e) }

// consumerMetrics are the metrics of the messages of one consumer.
type consumerMetrics struct {
	ack      prometheus.Counter
	requeue  prometheus.Counter
	discard  prometheus.Counter
	panicked prometheus.Counter
	duration prometheus.Observer
}

// newConsumerMetrics builds the metrics of the consumer name and registers them
// on reg. The consumers on one registry share the metrics, each under its own
// name; the counters of every action start at 0.
func newConsumerMetrics(reg prometheus.Registerer, name string) *consumerMetrics {
	consumed := prom.MustRegister(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name: "consumed_total",
		Help: "Messages handled, by consumer and action: ack, requeue, discard or panic.",
	}, []string{"consumer", "action"}))
	duration := prom.MustRegister(reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace, Subsystem: metricsSubsystem,
		Name:    "handle_duration_seconds",
		Help:    "Duration of the message handler, by consumer.",
		Buckets: prom.DurationBuckets(),
	}, []string{"consumer"}))
	return &consumerMetrics{
		ack:      consumed.WithLabelValues(name, "ack"),
		requeue:  consumed.WithLabelValues(name, "requeue"),
		discard:  consumed.WithLabelValues(name, "discard"),
		panicked: consumed.WithLabelValues(name, "panic"),
		duration: duration.WithLabelValues(name),
	}
}

// record records a message handled in d: a panic when the handler did not
// finish, else its action.
func (m *consumerMetrics) record(ctx context.Context, d time.Duration, action Action, finished bool) {
	prom.ObserveDuration(ctx, m.duration, d)
	switch {
	case !finished:
		m.panicked.Inc()
	case action == Requeue:
		m.requeue.Inc()
	case action == Discard:
		m.discard.Inc()
	default:
		m.ack.Inc()
	}
}
