package logger

import (
	"context"
	"log/slog"
	"testing"
)

// nopWriter drops the encoded bytes. It stands in for io.Discard, which New
// short-circuits: the benchmarks measure the full handler stack, encoding
// included.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func benchLogger() *slog.Logger {
	h := slog.NewJSONHandler(nopWriter{}, nil)
	return NewWithHandler(h, Config{Service: "svc", Version: "v1"})
}

// Allocation budget per record, checked by TestAllocBudget: a record without a
// span context costs what the JSON handler costs (none); a valid span context
// adds the hex strings of trace_id, span_id and trace_flags; a group adds the
// folded record. Measured 2026-10-17, go1.27.2.
const (
	allocsNoSpan  = 0
	allocsSpan    = 3
	allocsGrouped = 7
)

func BenchmarkNoSpan(b *testing.B) {
	log := benchLogger()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "request", "status", 200)
	}
}

func BenchmarkSpan(b *testing.B) {
	log := benchLogger()
	ctx := spanCtx(b)
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "request", "status", 200)
	}
}

func BenchmarkGrouped(b *testing.B) {
	log := benchLogger().WithGroup("http")
	ctx := spanCtx(b)
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "request", "status", 200)
	}
}

func TestAllocBudget(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation budget is checked without -race (make test runs it)")
	}
	log := benchLogger()
	grouped := log.WithGroup("http")
	span := spanCtx(t)
	tests := []struct {
		name   string
		log    *slog.Logger
		ctx    context.Context
		budget float64
	}{
		{"no span", log, context.Background(), allocsNoSpan},
		{"span", log, span, allocsSpan},
		{"grouped", grouped, span, allocsGrouped},
	}
	for _, tc := range tests {
		got := testing.AllocsPerRun(100, func() {
			tc.log.InfoContext(tc.ctx, "request", "status", 200)
		})
		if got > tc.budget {
			t.Errorf("%s: %.0f allocs per record, budget %.0f", tc.name, got, tc.budget)
		}
	}
}
