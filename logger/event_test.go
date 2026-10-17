package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/matryer/is"
)

// TestEventHookSeesAccumulatedContext verifies the Error hook fires with the
// FULL record: the per-call args, the accumulated With and service.*
// attributes (which live on the handler, not the record), and the trace
// fields from the span context.
func TestEventHookSeesAccumulatedContext(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	var got slog.Record
	var fired int

	log := New(&buf, Config{
		Service: "svc", Version: "v1", Level: LevelInfo,
		Events: Events{
			Error: func(_ context.Context, r slog.Record) {
				fired++
				got = r
			},
		},
	})

	log.With("logger", "access").With("region", "eu").ErrorContext(spanCtx(t), "boom", "code", 42)

	is.Equal(fired, 1)
	attrs := map[string]any{}
	got.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	is.Equal(attrs["service.name"], "svc")
	is.Equal(attrs["service.version"], "v1")
	is.Equal(attrs["logger"], "access")
	is.Equal(attrs["region"], "eu")
	is.Equal(attrs["code"], int64(42))
	is.Equal(attrs["trace_id"], testTraceID)
	is.Equal(attrs["span_id"], testSpanID)
	is.Equal(attrs["trace_flags"], "01")

	// The record is still written to the sink (hook is additive, not a replacement).
	is.True(strings.Contains(buf.String(), `"msg":"boom"`))
}

// TestEventHookFiresOnlyForConfiguredLevel checks that a hook set only for Error
// does not fire for Info, and that Info still writes normally.
func TestEventHookFiresOnlyForConfiguredLevel(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	var errFired int
	log := New(&buf, Config{
		Level: LevelInfo,
		Events: Events{Error: func(context.Context, slog.Record) {
			errFired++
		}},
	})
	ctx := context.Background()

	log.InfoContext(ctx, "hello")
	is.Equal(errFired, 0)
	log.ErrorContext(ctx, "oops")
	is.Equal(errFired, 1)
}

// TestEventHookOverMultiHandler confirms the hook composes with
// slog.NewMultiHandler passed to NewWithHandler: both sinks get the record and
// the hook still fires once.
func TestEventHookOverMultiHandler(t *testing.T) {
	is := is.New(t)
	var a, b bytes.Buffer
	var fired int
	multi := slog.NewMultiHandler(
		slog.NewJSONHandler(&a, &slog.HandlerOptions{Level: LevelInfo}),
		slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: LevelError}),
	)
	log := NewWithHandler(multi, Config{
		Service: "svc",
		Events:  Events{Error: func(context.Context, slog.Record) { fired++ }},
	})
	log.ErrorContext(spanCtx(t), "boom")

	is.Equal(fired, 1)
	for _, out := range []string{a.String(), b.String()} {
		is.True(strings.Contains(out, `"msg":"boom"`))
		is.True(strings.Contains(out, `"trace_id":"`+testTraceID+`"`))
		is.True(strings.Contains(out, `"service.name":"svc"`))
	}
}
