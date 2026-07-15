package logger

import (
	"context"
	"log/slog"
)

// EventFn is a side-effect callback invoked for a record at its level, IN
// ADDITION to normal handling — the record is still written to the underlying
// sink (including any FanoutHandler). Typical use: capture ERROR records to
// Sentry or fire an alert, while stdout logging is untouched.
//
// Unlike a lossy decoded snapshot, the callback receives the full slog.Record,
// reconstructed to include the attributes accumulated via With/Named and the
// service tag (which live on the handler, not the record) plus the trace_id, so
// the hook sees the same context the record is written with.
//
// The callback runs synchronously on the logging goroutine, before the write.
// Keep it fast: offload slow work (network calls) to a goroutine or buffered
// channel. Do NOT log at the hooked level from inside the callback — that
// re-enters the hook and recurses.
type EventFn func(ctx context.Context, r slog.Record)

// Events assigns an EventFn per level. The zero value (all nil) installs no
// hook; pass it in Config.Events. Hooks are additive to the sink, never a
// replacement for it — for multi-sink routing use a FanoutHandler.
type Events struct {
	Debug EventFn
	Info  EventFn
	Warn  EventFn
	Error EventFn
}

func (e Events) any() bool {
	return e.Debug != nil || e.Info != nil || e.Warn != nil || e.Error != nil
}

func (e Events) fnFor(level slog.Level) EventFn {
	switch level {
	case slog.LevelDebug:
		return e.Debug
	case slog.LevelInfo:
		return e.Info
	case slog.LevelWarn:
		return e.Warn
	case slog.LevelError:
		return e.Error
	default:
		return nil
	}
}

// groupOrAttrs records one WithAttrs or WithGroup step so the accumulated
// context can be replayed into the record handed to an EventFn. Exactly one of
// the fields is set (the canonical slog-handler bookkeeping).
type groupOrAttrs struct {
	group string      // non-empty ⇒ a WithGroup step
	attrs []slog.Attr // non-nil ⇒ a WithAttrs step
}

// eventHandler wraps a slog.Handler and, for records whose level has a
// configured EventFn, invokes that callback with the full record — the
// attributes gathered through With/Named/service replayed in — before
// delegating to the wrapped handler. It is otherwise transparent: Enabled and
// the eventual Handle are the wrapped handler's.
type eventHandler struct {
	next   slog.Handler
	events Events
	goas   []groupOrAttrs
}

func newEventHandler(next slog.Handler, events Events) *eventHandler {
	return &eventHandler{next: next, events: events}
}

func (h *eventHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *eventHandler) Handle(ctx context.Context, r slog.Record) error {
	if fn := h.events.fnFor(r.Level); fn != nil {
		fn(ctx, h.recordWithContext(r))
	}
	return h.next.Handle(ctx, r)
}

func (h *eventHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &eventHandler{
		next:   h.next.WithAttrs(attrs),
		events: h.events,
		goas:   appendGOA(h.goas, groupOrAttrs{attrs: attrs}),
	}
}

func (h *eventHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &eventHandler{
		next:   h.next.WithGroup(name),
		events: h.events,
		goas:   appendGOA(h.goas, groupOrAttrs{group: name}),
	}
}

// recordWithContext returns a clone of r with the accumulated With/Named/service
// attributes (and their group nesting) prepended, so the EventFn observes the
// record exactly as the sink will render it. trace_id is already on r (added by
// the outer traceHandler) and is preserved.
func (h *eventHandler) recordWithContext(r slog.Record) slog.Record {
	if len(h.goas) == 0 {
		return r
	}

	own := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		own = append(own, a)
		return true
	})

	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(foldGOA(h.goas, own)...)
	return out
}

// foldGOA replays the accumulated steps over the record's own attrs, honoring
// slog semantics: attributes added after a WithGroup are nested inside it.
func foldGOA(goas []groupOrAttrs, own []slog.Attr) []slog.Attr {
	if len(goas) == 0 {
		return own
	}
	ga := goas[0]
	if ga.group != "" {
		inner := foldGOA(goas[1:], own)
		anys := make([]any, len(inner))
		for i, a := range inner {
			anys[i] = a
		}
		return []slog.Attr{slog.Group(ga.group, anys...)}
	}
	rest := foldGOA(goas[1:], own)
	out := make([]slog.Attr, 0, len(ga.attrs)+len(rest))
	out = append(out, ga.attrs...)
	out = append(out, rest...)
	return out
}

func appendGOA(s []groupOrAttrs, ga groupOrAttrs) []groupOrAttrs {
	out := make([]groupOrAttrs, len(s)+1)
	copy(out, s)
	out[len(s)] = ga
	return out
}
