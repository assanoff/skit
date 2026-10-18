package queue_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/assanoff/skit/queue"
)

var errSend = errors.New("send failed")

// sendOnce fails every call after the first.
func sendOnce() queue.JobFunc {
	sent := false
	return func(context.Context, queue.Task) error {
		if sent {
			return errSend
		}
		sent = true
		return nil
	}
}

func ignore(context.Context, queue.Task) error { return nil }

const handledWant = `
# HELP skit_queue_handled_total Tasks handled, by kind and result: ok or error.
# TYPE skit_queue_handled_total counter
skit_queue_handled_total{kind="<unknown>",result="error"} 1
skit_queue_handled_total{kind="<unknown>",result="ok"} 0
skit_queue_handled_total{kind="email",result="error"} 1
skit_queue_handled_total{kind="email",result="ok"} 1
skit_queue_handled_total{kind="import",result="error"} 0
skit_queue_handled_total{kind="import",result="ok"} 0
`

// Every task is recorded under the kind it was registered with, a task of
// another kind under "<unknown>"; a registered kind starts at 0.
func TestMuxRecordsTasks(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	mux := queue.NewMux(queue.WithMetrics(reg))
	is.NoErr(mux.Register("email", sendOnce()))
	is.NoErr(mux.Register("import", ignore))

	is.NoErr(mux.Handle(t.Context(), queue.Task{Kind: "email"}))
	is.True(errors.Is(mux.Handle(t.Context(), queue.Task{Kind: "email"}), errSend))
	err := mux.Handle(t.Context(), queue.Task{Kind: "sms"})
	is.True(errors.Is(err, queue.ErrUnknownKind))

	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(handledWant), "skit_queue_handled_total"))
	n, err := testutil.GatherAndCount(reg, "skit_queue_handle_duration_seconds")
	is.NoErr(err)
	is.Equal(n, 3) // one duration series per kind
}

// With a fallback, a task of an unregistered kind is recorded as "<fallback>".
func TestMuxRecordsFallback(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	mux := queue.NewMux(queue.WithFallback(ignore), queue.WithMetrics(reg))

	is.NoErr(mux.Handle(t.Context(), queue.Task{Kind: "sms"}))

	want := `
# HELP skit_queue_handled_total Tasks handled, by kind and result: ok or error.
# TYPE skit_queue_handled_total counter
skit_queue_handled_total{kind="<fallback>",result="error"} 0
skit_queue_handled_total{kind="<fallback>",result="ok"} 1
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_queue_handled_total"))
}
