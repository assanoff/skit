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

func TestOutcomes(t *testing.T) {
	is := is.New(t)
	errDown := errors.New("down")

	is.True(isPermanent(Permanent(errDown)))       // Permanent marks the error
	is.True(isHalt(Halt(errDown)))                 // and so does Halt
	is.True(errors.Is(Halt(errDown), errDown))     // keeping the error it marks
	is.True(!isPermanent(errDown) && !isHalt(nil)) // an unmarked error is neither
	is.NoErr(Permanent(nil))
	is.NoErr(Halt(nil))
}

type order struct {
	ID int `json:"id"`
}

func TestDecode(t *testing.T) {
	is := is.New(t)
	var got order
	fn := Decode(func(_ context.Context, _ Task, v order) error {
		got = v
		return nil
	})

	is.NoErr(fn(t.Context(), Task{Payload: []byte(`{"id":7}`)}))
	is.Equal(got, order{ID: 7})                                       // the payload is decoded
	is.True(isPermanent(fn(t.Context(), Task{Payload: []byte(`{`)}))) // a bad one is Permanent
}

func TestMux(t *testing.T) {
	is := is.New(t)
	var handled []string
	record := func(_ context.Context, t Task) error {
		handled = append(handled, t.Kind)
		return nil
	}
	mux := NewMux()
	is.NoErr(mux.Register("email", record))
	is.True(mux.Register("email", record) != nil) // a duplicate kind is an error
	is.True(mux.Register("", record) != nil)      // and so is an empty one
	is.True(mux.Register("sms", nil) != nil)      // and a nil handler

	is.NoErr(mux.Handle(t.Context(), Task{Kind: "email"}))
	err := mux.Handle(t.Context(), Task{Kind: "push"})
	is.True(errors.Is(err, ErrUnknownKind)) // an unknown kind fails
	is.True(isPermanent(err))               // for good

	fallback := NewMux(WithFallback(record))
	is.NoErr(fallback.Handle(t.Context(), Task{Kind: "push"}))
	is.Equal(handled, []string{"email", "push"}) // the fallback takes unknown kinds
}

// The handler of a queue is recorded by queue and kind; a panic is an error
// and comes back as one.
func TestConsumerRecords(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	c := &consumer{
		h: func(_ context.Context, t Task) error {
			if t.Kind == "sms" {
				panic("boom")
			}
			return nil
		},
		metrics: newTaskMetrics(reg, "notify"),
	}

	is.NoErr(c.Handle(t.Context(), leased{Kind: "email"}))
	err := c.Handle(t.Context(), leased{Kind: "sms"})
	is.True(err != nil && strings.Contains(err.Error(), "panic: boom")) // the panic is an error

	want := `
# HELP skit_queue_handled_total Tasks handled, by queue, kind and result: ok or error.
# TYPE skit_queue_handled_total counter
skit_queue_handled_total{kind="email",queue="notify",result="ok"} 1
skit_queue_handled_total{kind="sms",queue="notify",result="error"} 1
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_queue_handled_total"))
}

func TestConfigDefaults(t *testing.T) {
	is := is.New(t)
	cfg := Config{Queue: "q"}.withDefaults()

	is.Equal(cfg, Config{Queue: "q", Concurrency: 1, Batch: 1, Timeout: 30 * time.Second, Poll: time.Second})
	is.Equal(cfg.lease(), 100*time.Second) // 3 × Timeout + 10s
}
