package broker

// Subscription is a transport-neutral description of what a consumer listens to.
// It lets application code (and the generated consumer scaffold) declare a
// subscription without naming a concrete broker; each transport adapter maps the
// generic fields onto its own vocabulary:
//
//	Subscription │ RabbitMQ    │ Kafka          │ NATS
//	─────────────┼─────────────┼────────────────┼───────────────
//	Topic        │ exchange    │ topic          │ subject
//	Group        │ queue       │ consumer group │ queue group
//	Filters      │ routing keys│ (ignored)      │ subject filters
//	Concurrency  │ goroutines  │ goroutines     │ goroutines
//
// Swapping transports is therefore a wiring change (which adapter's New you
// call), not a change to the handler or this subscription description.
type Subscription struct {
	// Name identifies the consumer in logs/metrics and, where the transport has
	// one, as its consumer tag / member id. Defaults are adapter-specific.
	Name string
	// Topic is the logical source to consume from (see the mapping above).
	Topic string
	// Group is the durable subscription name shared by replicas so a message is
	// delivered to the group once (RabbitMQ queue, Kafka consumer group, NATS
	// queue group).
	Group string
	// Filters select which messages within Topic reach this subscription
	// (RabbitMQ routing keys, NATS subject filters). Transports without the
	// concept ignore them.
	Filters []string
	// Concurrency is the number of handler goroutines (adapters default to 1).
	Concurrency int
}
