package metrics_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/cron"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/grpcserver"
	"github.com/assanoff/skit/httpclient"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/otel/setup"
	"github.com/assanoff/skit/outbox"
	"github.com/assanoff/skit/queue"
	"github.com/assanoff/skit/rest/mid"
)

// presets builds every SDK metric preset on one registry, without a network
// or a database, as a process that uses them all would.
func presets(t *testing.T) *metrics.Metrics {
	t.Helper()
	m := metrics.New()
	log := slog.New(slog.DiscardHandler)
	m.Middleware()
	grpcserver.New(log, grpcserver.Config{Metrics: m})
	mid.Chain(mid.Config{Metrics: m})
	if _, err := httpclient.New(httpclient.Config{Name: "billing", Metrics: m}); err != nil {
		t.Fatalf("httpclient: %v", err)
	}
	db, err := dbx.Open(dbx.Config{Host: "db", Name: "orders", Metrics: m}) // no connection yet
	if err != nil {
		t.Fatalf("dbx: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	outbox.NewRelay(log, nil, nil, outbox.RelayConfig{Metrics: m})
	outbox.NewSweeper(log, nil, outbox.SweeperConfig{Metrics: m})
	outbox.NewCleaner(log, nil, outbox.CleanerConfig{Metrics: m})
	outbox.NewBacklog(log, nil, m)
	queue.NewMux(queue.WithMetrics(m))
	queue.NewBacklog(log, &queue.PG{}, m)
	broker.Instrument(nil, broker.InstrumentConfig{Metrics: m})
	s := cron.New(log, cron.Config{Metrics: m})
	if err := s.Add("@hourly", "report", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("cron: %v", err)
	}
	tel, err := setup.New(t.Context(), log, setup.Config{
		Exporter: tracetest.NewInMemoryExporter(),
		Metrics:  m,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = tel.Shutdown() })
	return m
}

// fqName pulls the name out of Desc.String: a Desc has no other accessor.
var fqName = regexp.MustCompile(`fqName: "([^"]+)"`)

// names returns the names of the metrics registered on m, series or not:
// Describe reports a Vec that Gather would skip until its first series.
func names(t *testing.T, m *metrics.Metrics) map[string]bool {
	t.Helper()
	descs := make(chan *prometheus.Desc)
	go func() {
		m.Describe(descs)
		close(descs)
	}()
	out := make(map[string]bool)
	for d := range descs {
		match := fqName.FindStringSubmatch(d.String())
		if match == nil {
			t.Fatalf("no fqName in %s", d)
		}
		out[match[1]] = true
	}
	return out
}

// metricRef finds the metric names in a PromQL expression: the names of the
// SDK and of the client_golang collectors start with these prefixes, and no
// PromQL function or label of the dashboards does.
var metricRef = regexp.MustCompile(`\b(?:skit|go|process)_[a-z0-9_]+\b|\btarget_info\b`)

// exprs returns every PromQL string of a dashboard: the targets of the panels
// and the queries of the variables.
func exprs(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if s, ok := x.(string); ok && (k == "expr" || k == "query") {
					out = append(out, s)
				}
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(doc)
	return out
}

// known reports whether name is registered, as it is or as a series of a
// histogram or a summary.
func known(registered map[string]bool, name string) bool {
	if registered[name] {
		return true
	}
	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		if base, ok := strings.CutSuffix(name, suffix); ok && registered[base] {
			return true
		}
	}
	return false
}

// Every metric a dashboard queries exists: renaming a metric without its
// dashboards fails here.
func TestDashboardNames(t *testing.T) {
	registered := names(t, presets(t))
	paths, err := filepath.Glob("dashboards/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 6 {
		t.Fatalf("dashboards: %d files, want 6", len(paths))
	}
	for _, path := range paths {
		used := map[string]bool{}
		for _, e := range exprs(t, path) {
			for _, name := range metricRef.FindAllString(e, -1) {
				used[name] = true
			}
		}
		if len(used) == 0 {
			t.Errorf("%s queries no metric", path)
		}
		for _, name := range slices.Sorted(maps.Keys(used)) {
			if !known(registered, name) {
				t.Errorf("%s: %s is not a metric of the SDK", path, name)
			}
		}
	}
}

// The presets carry target_info and skit_otel_*, so the test covers the row
// of the telemetry health too.
func TestPresetsIncludeTelemetry(t *testing.T) {
	registered := names(t, presets(t))
	for _, name := range []string{"target_info", "skit_otel_exported_spans_total"} {
		if !registered[name] {
			t.Errorf("%s is not registered", name)
		}
	}
}
