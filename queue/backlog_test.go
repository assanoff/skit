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

// fakeStats returns st for the queue it was asked about, or err when set.
type fakeStats struct {
	st    stats
	err   error
	asked string
}

func (f *fakeStats) stats(_ context.Context, queue string, _ time.Time) (stats, error) {
	f.asked = queue
	return f.st, f.err
}

const backlogWant = `
# HELP skit_queue_dead Dead letters awaiting the cleaner.
# TYPE skit_queue_dead gauge
skit_queue_dead{queue="email"} 1
# HELP skit_queue_leased Tasks claimed by a consumer, including expired leases.
# TYPE skit_queue_leased gauge
skit_queue_leased{queue="email"} 2
# HELP skit_queue_pending Tasks ready to claim: not leased, run_at reached.
# TYPE skit_queue_pending gauge
skit_queue_pending{queue="email"} 7
`

// The gauges carry the backlog of their queue; a failed read keeps the gauges
// of the last successful one.
func TestBacklogRead(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	fakeSR := &fakeStats{st: stats{Pending: 7, Leased: 2, Dead: 1}}
	b := newBacklog(fakeSR, "email", reg)

	is.NoErr(b.read(t.Context()))
	is.Equal(fakeSR.asked, "email") // the backlog of its own queue
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(backlogWant)))

	fakeSR.st, fakeSR.err = stats{}, context.DeadlineExceeded
	err := b.read(t.Context())
	is.True(errors.Is(err, context.DeadlineExceeded))
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(backlogWant)))
}
