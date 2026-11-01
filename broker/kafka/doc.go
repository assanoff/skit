// Package kafka is the Apache Kafka adapter for the skit broker abstraction. It
// implements broker.Publisher and a worker.Runnable consumer over
// github.com/segmentio/kafka-go, exchanging CloudEvents v1.0 structured-mode
// JSON (broker.Marshal/UnmarshalCloudEvent) exactly like the rabbitmq adapter,
// so application code depends only on broker.Message / Handler / Publisher
// and never on the concrete transport.
//
// Semantics differ from AMQP where Kafka differs: delivery is at-least-once
// (manual offset commit after a successful handler); there is no per-message
// requeue, so a broker.Requeue verdict retries the same message in place, up to
// 5 times a second apart, before it is committed (dropped) — add a dead-letter
// topic for poison messages.
//
// # Partition key
//
// By default a record has no key, and the records of a topic spread over its
// partitions round-robin. WithPartitionKey derives a key from the whole message
// — usually the id of the aggregate, read from Data — and the records of one
// key go to one partition, in the order they are written:
//
//	pub, err := kafka.NewPublisher(cfg, kafka.PublisherConfig{Source: "myapp"},
//	    kafka.WithPartitionKey(func(m broker.Message) (string, error) {
//	        var v struct{ ID string `json:"id"` }
//	        if err := json.Unmarshal(m.Data, &v); err != nil {
//	            return "", broker.Permanent(err) // this message has no key
//	        }
//	        return v.ID, nil
//	    }))
//
// An error of the function fails the publish as it is: the queue of events
// retries the event, as it does for a broker that is down, and dead-letters it
// at once only for a broker.Permanent error. Wrap the error when the message
// itself has no key and never will; leave it as it is when the function may
// be wrong, so the events wait for a fixed one instead of failing.
//
// The order is best effort: the queue retries a failed event later, and the
// next events of its aggregate go ahead of it meanwhile.
//
// # Tracing
//
// Every publish is a span "send <topic>" with messaging.system kafka and the
// message key; a message without a trace context in its headers gets the
// PRODUCER span as its creation context, one that carries it is sent under a
// CLIENT child of it. NewPublisher takes the brokers in Config, the
// CloudEvents source and the TracerProvider in PublisherConfig, and the
// option WithPartitionKey.
//
// NewConsumer takes the brokers in Config and the Topic, Group, Concurrency,
// Name (default Group), Metrics and TracerProvider in ConsumerConfig. It records
// every attempt at a record as a CONSUMER span "process <topic>" in the trace
// the message was created in and in skit_broker_* (see broker).
package kafka
