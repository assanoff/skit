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

// The headers of a delivery are its string header values and its content
// type, under content-type.
func TestHeaders(t *testing.T) {
	is := is.New(t)
	is.Equal(headers(nil, ""), map[string]string(nil))
	got := headers(amqp.Table{"traceparent": "00-abc", "x-retries": int32(2)}, "application/json")
	is.Equal(got, map[string]string{
		"traceparent":  "00-abc", // non-strings are dropped
		"content-type": "application/json",
	})
}

// The content type of the wire headers goes in the content_type property, not
// in the header table.
func TestAMQPHeaders(t *testing.T) {
	is := is.New(t)
	table, contentType := amqpHeaders(map[string]string{
		"traceparent":  "00-abc",
		"content-type": "application/cloudevents+json",
	})
	is.Equal(table, rmq.Table{"traceparent": "00-abc"})
	is.Equal(contentType, "application/cloudevents+json")
}
