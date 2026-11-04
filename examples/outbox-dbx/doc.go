// Command outbox-dbx is the orders service of the outbox example: a write and
// its event commit in one Postgres transaction, and the workers of the queue
// of events publish the event to RabbitMQ afterwards. The domain in package
// orderapp sees neither dbx nor queue; the store in package orderdb opens the
// transaction; this assembly routes the event to its queue and topic.
//
// At start the service creates one order, pays it and reads it back, then
// runs the workers of the queue of events until a signal: they publish the
// event order.created to the exchange orders.
//
// # Usage
//
//	go run ./examples/outbox-dbx
//
// README.md runs Postgres and RabbitMQ in Docker and watches the exchange
// orders.
//
// # Options
//
// A flag or its environment variable:
//
//   - --db.host, DB_HOST: the Postgres host:port; localhost:5432 by default.
//   - --db.name, DB_NAME: the database; postgres by default.
//   - --db.user, DB_USER and --db.password, DB_PASSWORD: postgres by default.
//   - --broker.host, BROKER_HOST: the RabbitMQ host; localhost by default.
//   - --broker.port, BROKER_PORT: its AMQP port; 5672 by default.
//   - --broker.user, BROKER_USER and --broker.password, BROKER_PASSWORD:
//     guest by default.
package main
