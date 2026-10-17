package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/matryer/is"
	"go.opentelemetry.io/otel/trace"
)

const (
	testTraceID = "0102030405060708090a0b0c0d0e0f10"
	testSpanID  = "0102030405060708"
)

// spanCtx returns ctx carrying a valid, sampled remote span context.
func spanCtx(tb testing.TB) context.Context {
	tb.Helper()
	tid, err := trace.TraceIDFromHex(testTraceID)
	if err != nil {
		tb.Fatal(err)
	}
	sid, err := trace.SpanIDFromHex(testSpanID)
	if err != nil {
		tb.Fatal(err)
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled, Remote: true,
	})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

// lines decodes every JSON line written to buf.
func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for ln := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("decode %q: %v", ln, err)
		}
		out = append(out, m)
	}
	return out
}

func TestTraceFieldsFromSpanContext(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	log := New(&buf, Config{Level: LevelInfo})

	log.InfoContext(spanCtx(t), "with span")
	log.InfoContext(context.Background(), "without span")

	got := lines(t, &buf)
	is.Equal(len(got), 2)
	is.Equal(got[0]["trace_id"], testTraceID)
	is.Equal(got[0]["span_id"], testSpanID)
	is.Equal(got[0]["trace_flags"], "01")
	for _, k := range []string{"trace_id", "span_id", "trace_flags"} {
		_, ok := got[1][k]
		is.True(!ok) // no span context, no trace fields
	}
}

// TestTraceFieldsTopLevelUnderGroups checks that the trace fields stay at the
// top level under WithGroup, while attributes keep their slog nesting:
// before the group at the top, after it inside the group.
func TestTraceFieldsTopLevelUnderGroups(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	log := New(&buf, Config{Service: "svc", Level: LevelInfo})

	log.With("a", 1).WithGroup("g").With("b", 2).WithGroup("h").
		InfoContext(spanCtx(t), "grouped", "c", 3)

	got := lines(t, &buf)[0]
	is.Equal(got["trace_id"], testTraceID)
	is.Equal(got["span_id"], testSpanID)
	is.Equal(got["service.name"], "svc")
	is.Equal(got["a"], 1.0)
	g, ok := got["g"].(map[string]any)
	is.True(ok)
	is.Equal(g["b"], 2.0)
	h, ok := g["h"].(map[string]any)
	is.True(ok)
	is.Equal(h["c"], 3.0)
	_, ok = g["trace_id"]
	is.True(!ok) // not nested into the group
}

func TestEmptyGroupOmitted(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	log := New(&buf, Config{Level: LevelInfo})

	log.WithGroup("g").InfoContext(spanCtx(t), "no attrs")

	got := lines(t, &buf)[0]
	_, ok := got["g"]
	is.True(!ok) // slog drops a group without attributes
	is.Equal(got["trace_id"], testTraceID)
}

func TestServiceIdentity(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer

	New(&buf, Config{Service: "svc", Version: "v1.2.3", Level: LevelInfo}).
		InfoContext(context.Background(), "full")
	New(&buf, Config{Service: "svc", Level: LevelInfo}).
		InfoContext(context.Background(), "no version")

	got := lines(t, &buf)
	is.Equal(got[0]["service.name"], "svc")
	is.Equal(got[0]["service.version"], "v1.2.3")
	_, ok := got[1]["service.version"]
	is.True(!ok) // empty Version is not written
}

// TestWithSharesHandler verifies that a child from With writes through the
// parent's handler with the parent's identity, and only the child is tagged.
func TestWithSharesHandler(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	app := New(&buf, Config{Service: "svc", Level: LevelInfo})
	access := app.With("logger", "access")

	app.InfoContext(context.Background(), "business event")
	access.InfoContext(context.Background(), "http.request")

	got := lines(t, &buf)
	is.Equal(len(got), 2)
	is.Equal(got[0]["service.name"], "svc")
	is.Equal(got[1]["service.name"], "svc")
	_, ok := got[0]["logger"]
	is.True(!ok)
	is.Equal(got[1]["logger"], "access")
}

func TestDiscard(t *testing.T) {
	is := is.New(t)
	var fired int
	log := New(io.Discard, Config{
		Level:  LevelInfo,
		Events: Events{Error: func(context.Context, slog.Record) { fired++ }},
	})
	log.ErrorContext(context.Background(), "dropped")
	is.Equal(fired, 0) // no record is built, so no hook fires
}

func TestSourceIsShortened(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	New(&buf, Config{Level: LevelInfo, AddSource: true}).
		InfoContext(context.Background(), "here")

	src, _ := lines(t, &buf)[0]["source"].(string)
	is.True(strings.HasPrefix(src, "logger_test.go:")) // file:line, no directory
}

func TestBuildInfo(t *testing.T) {
	is := is.New(t)
	var buf bytes.Buffer
	BuildInfo(context.Background(), New(&buf, Config{Level: LevelInfo}))

	got := lines(t, &buf)[0]
	is.Equal(got["msg"], "build info")
	is.True(got["goversion"] != nil)
}
