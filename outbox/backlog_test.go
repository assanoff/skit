package outbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeStats returns st, or err when set.
type fakeStats struct {
	st  Stats
	err error
}

func (f *fakeStats) Stats(context.Context, time.Time) (Stats, error) {
	return f.st, f.err
}

const backlogWant = `
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

// A failed read keeps the gauges of the last successful one.
func TestBacklogRead(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	fakeSR := &fakeStats{st: Stats{
		Pending:          7,
		InFlight:         2,
		Failed:           1,
		OldestPendingAge: 90 * time.Second,
	}}
	b := newBacklog(fakeSR, reg)

	is.NoErr(b.read(t.Context()))
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(backlogWant)))

	fakeSR.st, fakeSR.err = Stats{}, context.DeadlineExceeded
	err := b.read(t.Context())
	is.True(errors.Is(err, context.DeadlineExceeded))
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(backlogWant)))
}

func TestNewBacklog(t *testing.T) {
	is := is.New(t)

	l := NewBacklog(nil, &fakeStats{}, prometheus.NewRegistry())

	is.Equal(l.Name(), "outbox-backlog")
}
