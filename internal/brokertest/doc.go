// Package brokertest starts the brokers of the skit tests in containers —
// RabbitMQ and a single-node Kafka — for the tests of broker/kafka,
// broker/rabbitmq and outbox. Pause freezes a container to stand for a broker
// that stops answering, and Raw is an envelope other than CloudEvents.
//
//	func TestPublish(t *testing.T) {
//	    if testing.Short() {
//	        t.Skip("integration test requires docker")
//	    }
//	    _, cfg := brokertest.RabbitMQ(t.Context(), t)
//	    conn, err := rabbitmq.Dial(cfg)
//	    // ...
//	}
//
// Each container is removed, and a paused one resumed, when the test ends.
package brokertest
