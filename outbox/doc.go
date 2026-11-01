// Package outbox publishes domain events to a message broker through a queue
// of events: an event is a task of the queue package, scheduled in the
// transaction of its domain change and published by the workers of its queue.
// So an event is never published unless its domain write commits, and never
// lost while the broker is down.
//
// The package is the bridge between the queue and the broker: Envelope writes
// an event into the Payload of its task, Handler publishes the task, and
// Config gives the settings of the queue of events. Storage, leases, retries,
// dead letters, cleanup and metrics belong to the queue.
//
// # Wiring (once, at startup)
//
// Register each event type in a queue.Registry with the Envelope encoder and
// the Handler of its topic in the Mux of the queue of events, check them
// against each other and run the workers of the queue:
//
//	reg := queue.NewRegistry()
//	enc := queue.WithEncoder(outbox.Envelope())
//	if err := reg.Register[widget.Created]("events", "widget.created", enc); err != nil {
//	    return err
//	}
//	mux := queue.NewMux()
//	if err := mux.Register("widget.created", outbox.Handler(pub, "widgets")); err != nil {
//	    return err
//	}
//	if err := reg.Check("events", mux); err != nil { // every event has a handler
//	    return err
//	}
//	workers, err := queue.Workers(log, q, outbox.Config("events"), mux.Handle,
//	    queue.WithMetrics(d.Metrics))
//	if err != nil {
//	    return err
//	}
//	for _, w := range workers {
//	    group.Add(w) // a worker.Group
//	}
//
// The kind of the task is the CloudEvents type of the event. The topic is the
// one of the Handler and transport-neutral: a RabbitMQ exchange, a Kafka
// topic; a key within it is the concern of the broker adapter
// (rabbitmq.WithRoutingKey, kafka.WithPartitionKey). With the broker off,
// register the event types with queue.Drop: the domain write goes through,
// and the event goes nowhere.
//
// # Producing events
//
// The store of the domain binds a queue.Scheduler to its transaction, so the
// event commits or rolls back with the domain write:
//
//	err := queue.Bind(tx, q, reg).Schedule(ctx, widget.Created{ID: w.ID})
//
// The domain declares its own interface with the same method,
// Schedule(ctx, v any) error, and imports neither queue nor dbx.
//
// # Envelope
//
// Envelope writes the event into the Payload as JSON: a fresh id, the time of
// Schedule, the content type of the codec, the data encoded by it — base64 in
// the JSON, whatever the codec — and the W3C trace context of the ctx of
// Schedule. The data is JSON unless WithCodec sets another codec, such as
// protobuf.
//
// # Delivery
//
// Handler publishes the message, and its error decides the outcome of the
// task:
//
//   - nil removes the task;
//   - an error marked broker.Permanent, or an envelope that does not decode,
//     sends it to the dead letters of the queue;
//   - any other error means the broker is likely down: queue.Halt retries the
//     task with backoff (2s × 2 up to 5 minutes) and stops the batch, whose
//     rest goes back untouched, so a broker that is down costs one failed
//     publish per poll.
//
// The queue of events has no limit on attempts, so a broker down for any time
// loses nothing; do not give it a backoff with MaxAttempts (queue.WithBackoff).
//
// Delivery is at-least-once: a process that dies after a publish but before
// the task is removed publishes it again after the lease. Consumers dedupe on
// the CloudEvents id, which the envelope keeps across retries. The order of the
// events of one aggregate is best effort: a retry with backoff and several
// replicas reorder them; a batch is published in run_at order.
//
// # Tracing
//
// The queue opens no spans: its ticks run suppressed (worker.Loop), so an idle
// queue records nothing, and each publish runs with the suppression lifted
// (worker.Processor). The envelope keeps the trace context the event was
// created in, and Handler passes it on in the headers of the message: the
// broker.Publisher of an adapter sends it under a CLIENT child of that context,
// and the consumer continues the same trace. A request, the event it created,
// its send and its handling are one trace.
//
// # Metrics
//
// The queue records them with queue.WithMetrics, labeled with the queue:
// skit_queue_handled_total by kind — the event type — and result,
// skit_queue_handle_duration_seconds, and the backlog gauges
// skit_queue_pending, skit_queue_leased and skit_queue_dead.
//
// # One payload, many events
//
// A Go type is one event type. When one payload goes out as several events
// chosen at runtime — e.g. a promocode whose discount kind decides the event —
// declare a type per event over the payload and register each. The choice
// stays domain logic:
//
//	type AmountCreated PushPayload
//	type PercentageCreated PushPayload
//
//	// reg.Register[AmountCreated]("events", "amount.promo.created", enc)
//	// reg.Register[PercentageCreated]("events", "percentage.promo.created", enc)
//	var ev any = AmountCreated(p)
//	if p.Percent {
//	    ev = PercentageCreated(p)
//	}
//	if err := s.Schedule(ctx, ev); err != nil {
//	    return err // any failure rolls back the whole unit of work
//	}
//
// # Options
//
//   - Envelope option: WithCodec (default JSON, application/json).
//   - Config(name): Batch 100, Timeout 5s (a publish and a batch), Poll 2s;
//     change a field when the broker needs it.
package outbox
