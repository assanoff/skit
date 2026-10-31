// Package outbox implements the transactional outbox pattern for reliably
// publishing domain events to a message broker.
//
// A domain write and the events it emits are inserted in ONE database
// transaction (Bind); a background Relay then drains pending events to a
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
// Producer side: a Registry maps each event's Go type to its event type and
// topic and encodes it with one Codec; a Publisher from Bind records typed
// values into the outbox in the caller's transaction, and one from Discard
// drops them. A key within the topic is the broker adapter's concern
// (rabbitmq.WithRoutingKey, kafka.WithPartitionKey).
//
// The Relay paces adaptively (worker.NewPacedLoop): a full batch means a backlog
// likely remains, so it drains again at once; an empty batch, or one stopped at
// a failed publish, idles for PollInterval (optionally backing off to
// MaxPollInterval).
//
// # Metrics
//
// The Metrics field of RelayConfig, SweeperConfig and CleanerConfig is the
// registry of the worker's metrics; nil disables them:
//   - the relay: skit_outbox_relay_published_total,
//     skit_outbox_relay_publish_failures_total and
//     skit_outbox_relay_publish_duration_seconds (a sampled publish carries its
//     trace as the exemplar);
//   - the sweeper: skit_outbox_sweeper_reclaimed_total;
//   - the cleaner: skit_outbox_cleaner_deleted_total.
//
// Each worker is a worker.Loop, so the same registry also gets its
// skit_worker_* by its Name.
//
// NewBacklog is the worker of the backlog gauges skit_outbox_pending,
// skit_outbox_in_flight, skit_outbox_failed and
// skit_outbox_oldest_pending_seconds: it reads them through a StatsReader
// (*PG) every 30 seconds. Add it only with a registry: it exists for its
// gauges. Every replica reports the same values, so a query takes max, not
// sum.
//
// # Event lifecycle (FSM)
//
//	 Insert
//	   │
//	   ▼
//	pending ──Lease──► in_flight ──MarkSent──► sent     (terminal)
//	   ▲                   │
//	   │ MarkFailed,       │
//	   │ Release, Sweep    │
//	   └───────────────────┤
//	                       └──MarkFailed (broker.Permanent)──► failed (terminal)
//
// A failed publish goes back to pending with backoff (up to 5 minutes) and no
// attempt limit; only an error the publisher marks broker.Permanent makes it
// failed. The first failed publish of a batch stops it, and Release returns the
// rest of the batch to pending untouched.
//
// At-least-once: if a relay dies after publishing but before MarkSent, the
// Sweeper returns the row to pending and it is republished; consumers dedupe on
// the CloudEvents id. All timestamps are computed Go-side (time.Now().UTC()) and
// bound as query parameters — no NOW()/CURRENT_TIMESTAMP in SQL — so the clock
// is injectable in tests.
//
// # Tracing
//
// Publish captures the W3C trace context of the publishing ctx — the context
// the event is created in — into the headers of the event (otel.Carrier). With
// no active trace context the headers stay empty.
//
// The relay opens no spans of its own. Its ticks run suppressed (worker.Loop),
// so an idle relay records nothing, and neither do the lease and the marks of
// the store. Each event is published with the suppression lifted
// (worker.Processor), and the relay passes its headers on to the broker
// message: the broker.Publisher of an adapter sends it under a CLIENT child of
// the creation context, and broker.Instrument on the consumer side continues
// the same trace. A request, the event it created, its send and its handling
// are one trace.
//
// # Wiring (once, at startup)
//
// Register each event type, then build the store and the workers:
//
//	reg := outbox.NewRegistry()
//	if err := reg.Register[widget.Created]("widget.created", "widgets"); err != nil {
//	    return err
//	}
//
//	store := outbox.NewPG(log, db, outbox.Options{})       // Postgres-backed Store
//	group.Add(outbox.NewRelay(log, store, publisher, outbox.RelayConfig{Metrics: d.Metrics}))
//	group.Add(outbox.NewSweeper(log, store, outbox.SweeperConfig{Metrics: d.Metrics}))
//	group.Add(outbox.NewCleaner(log, store, outbox.CleanerConfig{Metrics: d.Metrics}))
//	group.Add(outbox.NewBacklog(log, store, d.Metrics))
//
// # Producing events
//
// The transaction belongs to the store of the domain (dbx.WithinTran). The
// store hands the domain a Publisher bound to it: Bind writes the events on tx,
// so they commit or roll back with the domain writes. The wiring decides once
// where events go — into the outbox, or nowhere when the broker is off:
//
//	events := func(tx *dbx.Tx) *outbox.Publisher {
//	    if ob == nil { // the broker is off
//	        return outbox.Discard()
//	    }
//	    return outbox.Bind(tx, ob, reg)
//	}
//	store := widgetdb.NewStore(log, db, widgetdb.WithEvents(events))
//
// The domain declares its own Publisher interface with the same method, so it
// imports neither outbox nor dbx; *Publisher satisfies it without an adapter:
//
//	err := c.store.WithinTranEvents(ctx,
//	    func(r widget.Repo, pub widget.Publisher) error {
//	        if err := r.Create(ctx, w); err != nil { // domain write
//	            return err
//	        }
//	        return pub.Publish(ctx, widget.Created{ID: w.ID}) // domain event
//	    })
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
//	// reg.Register[AmountCreated]("amount.promo.created", "push-gateway")
//	// reg.Register[PercentageCreated]("percentage.promo.created", "push-gateway")
//	var ev any = AmountCreated(p)
//	if p.Percent {
//	    ev = PercentageCreated(p)
//	}
//	if err := pub.Publish(ctx, ev); err != nil {
//	    return err // any failure rolls back the whole unit of work
//	}
//
// # Options
//
//   - Registry option: WithCodec (default JSON, application/json).
//   - Store options (Options): Backoff (retry schedule for
//     failed-but-retryable events). The table is fixed (outbox_events).
//   - Worker configs: RelayConfig (PollInterval/MaxPollInterval/BatchSize/
//     PublishTimeout/Metrics), SweeperConfig (Interval/LeaseTimeout/BatchSize/
//     Metrics), CleanerConfig (Interval/Retention/Metrics).
//   - Backlog: NewBacklog(log, sr, metrics) (backlog gauges).
//
// Publisher (this package) writes events into the outbox table; it is distinct
// from broker.Publisher, which delivers an already-stored event to the wire.
package outbox
