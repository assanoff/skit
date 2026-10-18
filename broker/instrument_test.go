package broker_test

import (
	"context"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/assanoff/skit/broker"
)

// verdicts returns a handler that returns actions one by one and panics after
// the last.
func verdicts(actions ...broker.Action) broker.Handler {
	return func(context.Context, broker.Message) broker.Action {
		if len(actions) == 0 {
			panic("no verdict")
		}
		a := actions[0]
		actions = actions[1:]
		return a
	}
}

const consumedWant = `
# HELP skit_broker_consumed_total Messages handled, by consumer and action: ack, requeue, discard or panic.
# TYPE skit_broker_consumed_total counter
skit_broker_consumed_total{action="ack",consumer="orders"} 2
skit_broker_consumed_total{action="discard",consumer="orders"} 1
skit_broker_consumed_total{action="panic",consumer="orders"} 1
skit_broker_consumed_total{action="requeue",consumer="orders"} 1
`

// Every verdict is recorded under its action; a verdict outside the three is
// an ack, and a panic goes on to the Guard, which requeues the message.
func TestInstrumentRecordsVerdicts(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()
	h := broker.Instrument(
		verdicts(broker.Ack, broker.Requeue, broker.Discard, broker.Action(7)),
		broker.InstrumentConfig{Consumer: "orders", Metrics: reg},
	)
	body := ceBody(t, "order.paid")

	got := make([]broker.Action, 0, 5)
	for range 5 {
		got = append(got, broker.Process(t.Context(), nil, "orders", body, nil, h))
	}

	is.Equal(got, []broker.Action{
		broker.Ack, broker.Requeue, broker.Discard, broker.Action(7), broker.Requeue,
	})
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(consumedWant), "skit_broker_consumed_total"))
	n, err := testutil.GatherAndCount(reg, "skit_broker_handle_duration_seconds")
	is.NoErr(err)
	is.Equal(n, 1) // one duration series per consumer
}

// Before the first message every action is at 0, under the default name.
func TestInstrumentDefaultConsumer(t *testing.T) {
	is := is.New(t)
	reg := prometheus.NewRegistry()

	broker.Instrument(verdicts(), broker.InstrumentConfig{Metrics: reg})

	want := `
# HELP skit_broker_consumed_total Messages handled, by consumer and action: ack, requeue, discard or panic.
# TYPE skit_broker_consumed_total counter
skit_broker_consumed_total{action="ack",consumer="default"} 0
skit_broker_consumed_total{action="discard",consumer="default"} 0
skit_broker_consumed_total{action="panic",consumer="default"} 0
skit_broker_consumed_total{action="requeue",consumer="default"} 0
`
	is.NoErr(testutil.GatherAndCompare(reg, strings.NewReader(want), "skit_broker_consumed_total"))
}
