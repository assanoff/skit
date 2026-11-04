// Command rabbitmq is the orders service of the broker example. The domain in
// package orderapp knows only package broker; this assembly is the one place
// where RabbitMQ appears. At start the service publishes the event
// order.created of one order to the exchange orders, then consumes the event
// payment.paid from the queue orders.payments until a signal.
//
// # Usage
//
//	go run ./examples/rabbitmq
//
// README.md runs RabbitMQ in Docker, watches the exchange orders and sends a
// payment.
//
// # Options
//
// A flag or its environment variable:
//
//   - --broker.host, BROKER_HOST: the RabbitMQ host; localhost by default.
//   - --broker.port, BROKER_PORT: its AMQP port; 5672 by default.
//   - --broker.user, BROKER_USER and --broker.password, BROKER_PASSWORD:
//     guest by default.
package main
