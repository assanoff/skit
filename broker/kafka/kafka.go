package kafka

// Config holds the Kafka connection settings shared by the publisher and
// consumer. Unlike AMQP there is no long-lived shared connection: the writer and
// each reader dial the brokers themselves.
type Config struct {
	// Brokers is the bootstrap broker list, e.g. []string{"localhost:9092"}.
	Brokers []string
}
