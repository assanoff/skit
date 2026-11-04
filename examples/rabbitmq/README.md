# rabbitmq: the domain knows only `broker`

An orders service that publishes an event and consumes another one over
RabbitMQ. The domain sees the contract of package `broker` — a
`broker.Publisher` and a `broker.Handler` — and never the transport; RabbitMQ
appears in one place, the assembly in `main.go`.

| file | what it holds |
|---|---|
| [`orderapp/orderapp.go`](orderapp/orderapp.go) | the domain: `Create` publishes `order.created` to the topic `orders`; `HandlePaid` is the `broker.Handler` of `payment.paid`. It imports `broker` only |
| [`main.go`](main.go) | the assembly: `newDeps` connects and builds the publisher and starts nothing; `addConsumers` registers the consumer of the queue `orders.payments` in the `worker.Group`; `run` shows what is built and what is run |

## Run it

Start RabbitMQ with its management API:

```sh
docker run -d --name skit-rabbit -p 5672:5672 -p 15672:15672 \
  rabbitmq:3.13-management-alpine
```

A publisher declares no exchange: exchanges are infrastructure, or the
consumers declare them (the consumer of this service declares `payments` on
start). Declare the exchange `orders` and a queue to watch it:

```sh
api=localhost:15672/api
curl -u guest:guest -H 'content-type: application/json' \
  -X PUT $api/exchanges/%2F/orders -d '{"type":"topic","durable":true}'
curl -u guest:guest -H 'content-type: application/json' \
  -X PUT $api/queues/%2F/orders.audit -d '{"durable":true}'
curl -u guest:guest -H 'content-type: application/json' \
  -X POST $api/bindings/%2F/e/orders/q/orders.audit -d '{"routing_key":"#"}'
```

Without the exchange the broker refuses the event, and the service stops with
`orderapp: create: rabbitmq publish: broker nacked the message`.

Run the service; it publishes `order.created` of one order and waits for
payments:

```sh
go run ./examples/rabbitmq
# level=INFO msg="order created" order_id=5GSTFKLCVPVA5QXXNYIW6M2XHD
```

Pay the order: publish `payment.paid` in the CloudEvents envelope, with the id
from the log:

```sh
order=5GSTFKLCVPVA5QXXNYIW6M2XHD
curl -u guest:guest -H 'content-type: application/json' \
  -X POST $api/exchanges/%2F/payments/publish -d '{
  "routing_key": "payment.paid",
  "properties": {"content_type": "application/cloudevents+json"},
  "payload_encoding": "string",
  "payload": "{\"specversion\":\"1.0\",\"id\":\"p-1\",\"type\":\"payment.paid\",\"source\":\"billing\",\"datacontenttype\":\"application/json\",\"data\":{\"order_id\":\"'$order'\"}}"
}'
# the service: level=INFO msg="order paid" order_id=5GSTFKLCVPVA5QXXNYIW6M2XHD
```

A payment that names no order is discarded with a warning: delivered again, it
would fail again. Read the event of the order:

```sh
curl -u guest:guest -H 'content-type: application/json' \
  -X POST $api/queues/%2F/orders.audit/get \
  -d '{"count":10,"ackmode":"ack_requeue_false","encoding":"auto"}'
```

Another host or port: `BROKER_HOST`, `BROKER_PORT`, `BROKER_USER`,
`BROKER_PASSWORD` or the flags of `go run ./examples/rabbitmq --help`. Ctrl+C
stops the consumer, then closes the publisher and the connection. Clean up
with `docker rm -f skit-rabbit`.

## Kafka instead of RabbitMQ

Only the assembly changes: `newDeps` builds `kafka.NewPublisher` and
`addConsumers` builds `kafka.NewConsumer` — the
[`Example`](../../broker/kafka/example_test.go) of `broker/kafka`. The domain
stays: its contract lives in `broker`. The key is the assembly's choice too:
RabbitMQ routes by the `Type` of a message (`rabbitmq.WithRoutingKey` routes
otherwise), Kafka writes records without a key, and `kafka.WithPartitionKey`
keeps the events of one order in order — `ExampleWithPartitionKey` there.

## An envelope of your own

A message goes out in the `broker.CloudEvents` envelope (structured JSON). Any
other format — binary CloudEvents, the envelope of an older system, a bare
payload with its attributes in the headers — is a `broker.Envelope` of the
service, given to the publisher and to the consumers of one stream with
`WithEnvelope`:

```go
// legacy is a bare payload with the type and the id in the headers.
type legacy struct{}

func (legacy) Marshal(m broker.Message) ([]byte, map[string]string, error) {
	return m.Data, map[string]string{
		"content-type": m.ContentType,
		"x-event-id":   m.ID,
		"x-event-type": m.Type,
	}, nil
}

func (legacy) Unmarshal(body []byte, h map[string]string) (broker.Message, error) {
	if h["x-event-type"] == "" {
		return broker.Message{}, errors.New("legacy: no event type")
	}
	return broker.Message{
		ID: h["x-event-id"], Type: h["x-event-type"],
		ContentType: h["content-type"], Data: body,
	}, nil
}
```

```go
pub, err := rabbitmq.NewPublisher(conn, rabbitmq.PublisherConfig{Source: "orders"},
	rabbitmq.WithEnvelope(legacy{}))
cons, err := rabbitmq.NewConsumer(log, d.Conn, rabbitmq.ConsumerConfig{Queue: "orders.payments"},
	app.HandlePaid, rabbitmq.WithEnvelope(legacy{}))
```

The adapter puts the headers of the envelope over `Message.Headers`, so the
trace context survives. RabbitMQ carries `content-type` in the property
`content_type`, Kafka keeps it a header. A consumer gets every header of the
delivery in `Message.Headers` and rejects, with a warning in the log, a
message its envelope cannot unpack. The adapter returns an error of `Marshal`
as it is: wrap it in `broker.Permanent` when the message can never be packed.
