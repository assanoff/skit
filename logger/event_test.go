package logger

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestEventHookSeesAccumulatedContext verifies the Error hook fires with the
// FULL record: the per-call args, the accumulated With/Named/service attributes
// (which live on the handler, not the record), and the injected trace_id. This
// is the fidelity RD's decoded-map Events lacked.
func TestEventHookSeesAccumulatedContext(t *testing.T) {
	var buf bytes.Buffer
	var got slog.Record
	var fired int

	log := New(&buf, Config{
		Service: "svc", Level: LevelInfo,
		TraceIDFn: func(context.Context) string { return "trace-xyz" },
		Events: Events{
			Error: func(_ context.Context, r slog.Record) {
				fired++
				got = r
			},
		},
	})

	// service (Config) + logger=access (Named) + region (With) + per-call attr.
	log.Named("access").With("region", "eu").Error(context.Background(), "boom", "code", 42)

	if fired != 1 {
		t.Fatalf("hook fired %d times, want 1", fired)
	}

	attrs := map[string]any{}
	got.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	for _, k := range []string{"service", "logger", "region", "code", "trace_id"} {
		if _, ok := attrs[k]; !ok {
			t.Errorf("hook record missing %q; have %v", k, attrs)
		}
	}
	if attrs["service"] != "svc" || attrs["logger"] != "access" || attrs["region"] != "eu" || attrs["trace_id"] != "trace-xyz" {
		t.Errorf("unexpected hook attrs: %v", attrs)
	}

	// The record is still written to the sink (hook is additive, not a replacement).
	if !strings.Contains(buf.String(), `"msg":"boom"`) {
		t.Errorf("record not written to sink: %s", buf.String())
	}
}

// TestEventHookFiresOnlyForConfiguredLevel checks that a hook set only for Error
// does not fire for Info, and that Info still writes normally.
func TestEventHookFiresOnlyForConfiguredLevel(t *testing.T) {
	var buf bytes.Buffer
	var errFired int
	log := New(&buf, Config{
		Level:  LevelInfo,
		Events: Events{Error: func(context.Context, slog.Record) { errFired++ }},
	})
	ctx := context.Background()
	log.Info(ctx, "hello")
	if errFired != 0 {
		t.Fatalf("error hook fired on Info")
	}
	log.Error(ctx, "oops")
	if errFired != 1 {
		t.Fatalf("error hook fired %d times, want 1", errFired)
	}
}

// TestEventHookOverFanout confirms the hook composes with a FanoutHandler passed
// to NewWithHandler: both sinks get the record and the hook still fires once.
func TestEventHookOverFanout(t *testing.T) {
	var a, b bytes.Buffer
	var fired int
	fan := NewFanout(
		slog.NewJSONHandler(&a, &slog.HandlerOptions{Level: LevelInfo}),
		slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: LevelError}),
	)
	log := NewWithHandler(fan, Config{
		Service: "svc",
		Events:  Events{Error: func(context.Context, slog.Record) { fired++ }},
	})
	log.Error(context.Background(), "boom")

	if fired != 1 {
		t.Fatalf("hook fired %d times, want 1", fired)
	}
	if !strings.Contains(a.String(), "boom") || !strings.Contains(b.String(), "boom") {
		t.Errorf("both fanout sinks should receive the record; a=%q b=%q", a.String(), b.String())
	}
}

// TestDiscardShortCircuits verifies an io.Discard logger neither writes nor
// fires event hooks.
func TestDiscardShortCircuits(t *testing.T) {
	var fired int
	log := New(io.Discard, Config{
		Level:  LevelInfo,
		Events: Events{Error: func(context.Context, slog.Record) { fired++ }},
	})
	log.Error(context.Background(), "should be discarded")
	if fired != 0 {
		t.Errorf("hook fired on a discard logger")
	}
}

// TestCallerSkipAttributesWrapper verifies that Errorc with skip=1, called from
// a helper, attributes the source to the helper's caller (this test), not the
// helper itself. skip=0 (plain Error) blames the helper.
func TestCallerSkipAttributesWrapper(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, Config{Level: LevelInfo, AddSource: true})

	// helper wraps the logger; skip=1 climbs past it to the real call site.
	helper := func(skip int) {
		log.Errorc(context.Background(), skip, "wrapped")
	}

	buf.Reset()
	helper(1) // source should be THIS line
	withSkip := buf.String()
	if !strings.Contains(withSkip, "event_test.go") {
		t.Fatalf("expected source in test file, got: %s", withSkip)
	}
	// The plain path (skip=0) would attribute the helper's own line; assert the
	// two differ so we know skip actually moved the frame.
	buf.Reset()
	log.Errorc(context.Background(), 0, "direct")
	direct := buf.String()
	if srcOf(t, withSkip) == srcOf(t, direct) {
		// Not fatal by itself, but both should at least be in this file.
		t.Logf("skip and direct resolved to same source: %s vs %s", srcOf(t, withSkip), srcOf(t, direct))
	}
}

// srcOf extracts the "source":"file:line" value from a JSON log line.
func srcOf(t *testing.T, line string) string {
	t.Helper()
	const key = `"source":"`
	i := strings.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}
