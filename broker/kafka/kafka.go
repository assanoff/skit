// Package kafka is the Apache Kafka adapter for the skit broker abstraction. It
// implements broker.Publisher and a worker.Runnable consumer over
// github.com/segmentio/kafka-go, exchanging CloudEvents v1.0 structured-mode
// JSON (broker.Marshal/UnmarshalCloudEvent) exactly like the rabbitmq adapter,
// so application code depends only on broker.Message / Handler / Subscription
// and never on the concrete transport.
//
// Semantics differ from AMQP where Kafka differs: delivery is at-least-once
// (manual offset commit after a successful handler); there is no per-message
// requeue, so a broker.Requeue verdict retries the same message in place with a
// bounded backoff before it is committed (dropped) — add a dead-letter topic for
// poison messages. Subscription.Filters has no Kafka equivalent and is ignored.
package kafka

// Config holds the Kafka connection settings shared by the publisher and
// consumer. Unlike AMQP there is no long-lived shared connection: the writer and
// each reader dial the brokers themselves.
type Config struct {
	// Brokers is the bootstrap broker list, e.g. []string{"localhost:9092"}.
	Brokers []string
}
