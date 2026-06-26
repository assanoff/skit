package logger

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

type ctxKey struct{}

// TestTraceInjectedAtHandler verifies trace_id is added by the handler wrapper,
// so it appears both through *Logger and through a plain *slog.Logger from
// Slog() (the path third-party middleware like httplog uses).
func TestTraceInjectedAtHandler(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, Config{
		Service: "svc", Level: LevelInfo,
		TraceIDFn: func(ctx context.Context) string {
			if v, ok := ctx.Value(ctxKey{}).(string); ok {
				return v
			}
			return ""
		},
	})
	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-123")

	log.Info(ctx, "via Logger")
	log.Slog().InfoContext(ctx, "via Slog")

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), out)
	}
	for i, ln := range lines {
		if !strings.Contains(ln, `"trace_id":"trace-123"`) {
			t.Errorf("line %d missing trace_id: %s", i, ln)
		}
	}
}

// TestNamedSharesHandler verifies that Named derives a tagged instance writing
// through the same handler (same buffer here), so a single sink serves multiple
// logger instances.
func TestNamedSharesHandler(t *testing.T) {
	var buf bytes.Buffer
	app := New(&buf, Config{Service: "svc", Level: LevelInfo})
	access := app.Named("access")

	ctx := context.Background()
	app.Info(ctx, "business event")
	access.Info(ctx, "http.request")

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines through the shared handler, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], `"service":"svc"`) {
		t.Errorf("app line missing service tag: %s", lines[0])
	}
	if strings.Contains(lines[0], `"logger":"access"`) {
		t.Errorf("app line should NOT carry the access tag: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"logger":"access"`) {
		t.Errorf("access line should carry logger=access: %s", lines[1])
	}
	// Both instances still carry the shared service tag (same handler attrs).
	if !strings.Contains(lines[1], `"service":"svc"`) {
		t.Errorf("access line missing shared service tag: %s", lines[1])
	}
}
