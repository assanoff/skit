package outbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeStats struct {
	st  Stats
	err error
}

func (f fakeStats) Stats(context.Context, time.Time) (Stats, error) { return f.st, f.err }

func TestBacklogCollectorEmitsGauges(t *testing.T) {
	sr := fakeStats{st: Stats{
		Pending:          7,
		InFlight:         2,
		Failed:           1,
		OldestPendingAge: 90 * time.Second,
	}}
	c := NewBacklogCollector(sr, nil)

	want := `
# HELP skit_outbox_failed Events in the terminal failed state.
# TYPE skit_outbox_failed gauge
skit_outbox_failed 1
# HELP skit_outbox_in_flight Events leased by a relay, publish in progress.
# TYPE skit_outbox_in_flight gauge
skit_outbox_in_flight 2
# HELP skit_outbox_oldest_pending_seconds Age in seconds of the oldest pending event (0 when none).
# TYPE skit_outbox_oldest_pending_seconds gauge
skit_outbox_oldest_pending_seconds 90
# HELP skit_outbox_pending Events awaiting the relay (status=pending).
# TYPE skit_outbox_pending gauge
skit_outbox_pending 7
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Errorf("unexpected metrics:\n%v", err)
	}
}

func TestBacklogCollectorEmitsNothingOnError(t *testing.T) {
	c := NewBacklogCollector(fakeStats{err: context.DeadlineExceeded}, nil)
	if n := testutil.CollectAndCount(c); n != 0 {
		t.Errorf("expected 0 metrics on stats error, got %d", n)
	}
}

func TestOutboxMetricsRegisterAndRecord(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.observePublish(5*time.Millisecond, nil)
	m.observePublish(0, context.DeadlineExceeded)
	m.addSwept(3)
	m.addCleaned(4)

	if got := testutil.ToFloat64(m.published); got != 1 {
		t.Errorf("published = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.publishFailed); got != 1 {
		t.Errorf("publishFailed = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.swept); got != 3 {
		t.Errorf("swept = %v, want 3", got)
	}
	if got := testutil.ToFloat64(m.cleaned); got != 4 {
		t.Errorf("cleaned = %v, want 4", got)
	}
}

// A nil *Metrics must be a safe no-op so metrics stay optional.
func TestNilMetricsNoop(t *testing.T) {
	var m *Metrics
	m.observePublish(time.Second, nil)
	m.addSwept(1)
	m.addCleaned(1)
}
