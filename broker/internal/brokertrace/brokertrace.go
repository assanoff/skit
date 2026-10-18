// Package brokertrace records the send span of the broker adapters.
package brokertrace

import (
	"context"
	"maps"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/otel"
)

// StartSend opens the span "send <destination>" of one message and returns
// the headers to send it with. When headers carry no trace context, the span
// is a PRODUCER: it is the creation context of the message, and the returned
// headers carry it. When they carry one — the outbox relay sends the context
// the event was created in — the span is a CLIENT child of that context and
// the headers go out as they are. attrs describe the system, the message id
// and what the transport adds.
func StartSend(
	ctx context.Context, tracer trace.Tracer, destination string,
	headers map[string]string, attrs ...attribute.KeyValue,
) (context.Context, trace.Span, map[string]string) {
	attrs = append(attrs,
		semconv.MessagingOperationName("send"),
		semconv.MessagingOperationTypeSend,
		semconv.MessagingDestinationName(destination),
	)
	kind, parent := trace.SpanKindProducer, ctx
	created := Created(headers)
	if created.IsValid() {
		kind, parent = trace.SpanKindClient, trace.ContextWithRemoteSpanContext(ctx, created)
	}
	ctx, span := tracer.Start(parent, "send "+destination,
		trace.WithSpanKind(kind), trace.WithAttributes(attrs...))
	if created.IsValid() {
		return ctx, span, headers
	}
	out := maps.Clone(headers)
	if out == nil {
		out = make(map[string]string)
	}
	maps.Copy(out, otel.Carrier(ctx))
	return ctx, span, out
}

// Created returns the creation context of a message from its headers; it is
// invalid when the headers carry none.
func Created(headers map[string]string) trace.SpanContext {
	return trace.SpanContextFromContext(otel.ExtractFromCarrier(context.Background(), headers))
}

// End ends span, marked failed by err when it is not nil.
func End(span trace.Span, err error) {
	otel.SetError(span, err)
	span.End()
}
