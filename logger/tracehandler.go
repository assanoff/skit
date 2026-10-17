package logger

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// traceHandler is a slog.Handler middleware that adds trace_id, span_id and
// trace_flags from the span context in ctx to every record whose context
// carries a valid one. The fields always land at the top level of the record,
// so a collector finds them under WithGroup too.
//
// Attributes added before the first WithGroup go straight to next, which keeps
// their pre-rendered form. From the first WithGroup on, the steps are kept here
// and folded into the record in Handle: next never sees the groups, and the
// trace fields are added outside them.
type traceHandler struct {
	next slog.Handler
	goas []groupOrAttrs // steps from the first WithGroup on
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if len(h.goas) > 0 {
		r = foldRecord(h.goas, r)
	}
	if sc.IsValid() {
		r = r.Clone()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
			slog.String("trace_flags", sc.TraceFlags().String()),
		)
	}
	return h.next.Handle(ctx, r)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	if len(h.goas) == 0 {
		return &traceHandler{next: h.next.WithAttrs(attrs)}
	}
	return &traceHandler{next: h.next, goas: appendGOA(h.goas, groupOrAttrs{attrs: attrs})}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &traceHandler{next: h.next, goas: appendGOA(h.goas, groupOrAttrs{group: name})}
}

// foldRecord returns a copy of r whose attributes are nested in the kept
// WithGroup/WithAttrs steps, as next would have nested them.
func foldRecord(goas []groupOrAttrs, r slog.Record) slog.Record {
	own := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		own = append(own, a)
		return true
	})
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(foldGOA(goas, own)...)
	return out
}
