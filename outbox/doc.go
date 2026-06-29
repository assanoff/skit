// Package outbox implements the transactional outbox pattern for reliably
// publishing domain events to a message broker.
//
// A domain write and the events it emits are inserted in ONE database
// transaction (WithinTran); a background Relay then drains pending events to a
// broker.Publisher with at-least-once delivery. So an event is never lost if
// the broker is briefly down, and never published unless its domain write
// commits.
//
// # Design goals
//
// The domain layer stays oblivious to persistence and transport. Producers
// publish a plain typed value through a tx-bound Publisher; which broker topic
// it lands on, and over which transport, are wiring concerns resolved by a
// Registry — never named in domain code. Routing is transport-neutral (Topic +
// optional Key), so the same event row can ship over RabbitMQ, Kafka, NATS, or
// a WebSocket gateway by swapping the broker.Publisher behind the Relay.
//
// # Components
//
// The moving parts map onto the worker package:
//
//   - Relay   = worker.Processor[Event]: LeasePending (Source) -> broker.Publish
//     (Handler) -> MarkSent/MarkFailed (Sink), run as a worker.Loop.
//   - Sweeper = worker.Loop reclaiming leases abandoned by crashed relays.
//   - Cleaner = worker.Loop deleting terminal rows past a retention window.
//
// Producer side: a Registry maps each event's Go type to a route; Publisher
// (obtained from WithinTran or Bind) records typed values into the outbox in
// the caller's transaction; PublishOption overrides the route per call.
//
// The Relay paces adaptively (worker.NewPacedLoop): a full batch means a backlog
// likely remains, so it drains again at once; an empty batch idles for
// PollInterval (optionally backing off to MaxPollInterval).
//
// Metrics are optional and registered on the shared registry (see the metrics
// package): NewMetrics builds relay/sweeper/cleaner throughput counters
// (skit_outbox_*), wired via the *Metrics field on RelayConfig/
// SweeperConfig/CleanerConfig; NewBacklogCollector exposes backlog gauges
// (pending/in_flight/failed/oldest_pending_seconds) by querying a StatsReader on
// each scrape.
//
// # Event lifecycle (FSM)
//
//	 Insert
//	   │
//	   ▼
//	pending ──Lease──► in_flight ──MarkSent──► sent     (terminal)
//	   ▲                   │
//	   │ MarkFailed/Sweep │ MarkFailed (attempts >= max)
//	   ──────────────────┴──────────► failed           (terminal)
//
// At-least-once: if a relay dies after publishing but before MarkSent, the
// Sweeper returns the row to pending and it is republished; consumers dedupe on
// the CloudEvents id. All timestamps are computed Go-side (time.Now().UTC()) and
// bound as query parameters — no NOW()/CURRENT_TIMESTAMP in SQL — so the clock
// is injectable in tests.
//
// Tracing: Publish captures the W3C trace context from the publishing ctx into
// the event's headers (otel.Carrier), so the relay carries it onto the broker
// message and a consumer can continue the producer's trace with
// otel.ExtractFromCarrier. With no active trace context the headers stay empty.
//
// # Wiring (once, at startup)
//
// Register each event type, then build the store and the workers:
//
//	reg := outbox.NewRegistry()
//	if err := outbox.Register[widget.Created](reg, "widget.created", "widgets",
//	    outbox.WithKey("created")); err != nil {
//	    return err
//	}
//
//	store := outbox.NewPG(log, db, outbox.Options{})       // Postgres-backed Store
//	group.Add(outbox.NewRelay(log, store, publisher, outbox.RelayConfig{}))
//	group.Add(outbox.NewSweeper(log, store, outbox.SweeperConfig{}))
//	group.Add(outbox.NewCleaner(log, store, outbox.CleanerConfig{}))
//
// # Producing events
//
// Inside WithinTran the domain writes and publishes in the same transaction;
// any error rolls back both. The closure receives tx (so an application can
// bind its own store: store.WithTx(tx)) and a tx-bound Publisher:
//
//	err := outbox.WithinTran(ctx, log, db, store, reg,
//	    func(tx *sqlx.Tx, pub outbox.Publisher) error {
//	        if err := repo.WithTx(tx).Create(ctx, w); err != nil { // domain write
//	            return err
//	        }
//	        return pub.Publish(ctx, widget.Created{ID: w.ID.String()}) // domain event
//	    })
//
// # One type, many events (PublishOption)
//
// When one payload type fans out to several CloudEvents types chosen at runtime
// — e.g. a promocode whose discount kind decides the event — register a default
// route and override it per publish with As (and optionally OnTopic /
// WithRouteKey). The routing-by-condition stays domain logic:
//
//	// Register[PushPayload](reg, "promo.created", "push-gateway", WithKey("push-gateway.send"))
//	for _, batch := range batches { // fan-out: N events, one transaction
//	    if err := pub.Publish(ctx, PushPayload{...}, outbox.As(eventType)); err != nil {
//	        return err // any failure rolls back the whole unit of work
//	    }
//	}
//
// # Options
//
//   - Register options: WithKey, WithContentType (default application/json),
//     WithMarshaler (default encoding/json).
//   - Publish options: As (override CloudEvents type), OnTopic (override topic),
//     WithRouteKey (override key).
//   - Store options (Options): Backoff (retry schedule for
//     failed-but-retryable events). The table is fixed (outbox_events).
//   - Worker configs: RelayConfig (PollInterval/MaxPollInterval/BatchSize/
//     PublishTimeout/Metrics), SweeperConfig (Interval/LeaseTimeout/BatchSize/
//     Metrics), CleanerConfig (Interval/Retention/Metrics).
//   - Metrics: NewMetrics(reg) (throughput counters), NewBacklogCollector(sr,
//     log) (backlog gauges), WithScrapeTimeout.
//
// # Escape hatch
//
// For a fully dynamic event built without the Registry, construct it with
// NewEvent and insert it on the transaction directly:
//
//	ev, _ := outbox.NewEvent(eventType, topic, key, "application/json", payload, nil)
//	err := store.WithTx(tx).Insert(ctx, ev)
//
// Publisher (this package) writes events into the outbox table; it is distinct
// from broker.Publisher, which delivers an already-stored event to the wire.
package outbox
