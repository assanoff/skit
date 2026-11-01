package rabbitmq

import (
	"testing"

	"github.com/matryer/is"
	amqp "github.com/rabbitmq/amqp091-go"
	rmq "github.com/wagslane/go-rabbitmq"

	"github.com/assanoff/skit/broker"
)

func TestToAction(t *testing.T) {
	is := is.New(t)
	is.Equal(toAction(broker.Ack), rmq.Ack)
	is.Equal(toAction(broker.Requeue), rmq.NackRequeue)
	is.Equal(toAction(broker.Discard), rmq.NackDiscard)
}

func TestHeaders(t *testing.T) {
	is := is.New(t)
	is.Equal(headers(nil), map[string]string(nil))
	got := headers(amqp.Table{"traceparent": "00-abc", "x-retries": int32(2)})
	is.Equal(got, map[string]string{"traceparent": "00-abc"}) // non-strings are dropped
}
