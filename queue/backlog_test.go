package queue

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
# HELP skit_queue_dead Dead letters awaiting Cleanup.
# TYPE skit_queue_dead gauge
skit_queue_dead 1
# HELP skit_queue_leased Tasks claimed by a consumer, including expired leases.
# TYPE skit_queue_leased gauge
skit_queue_leased 2
# HELP skit_queue_pending Tasks ready to claim: not leased, run_at reached.
# TYPE skit_queue_pending gauge
skit_queue_pending 7
`

// A failed read keeps the gauges of the last successful one.
func TestBacklogRead(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	fakeSR := &fakeStats{st: Stats{Pending: 7, Leased: 2, Dead: 1}}
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

	l := NewBacklog(nil, &PG{}, prometheus.NewRegistry())

	is.Equal(l.Name(), "queue-backlog")
}
