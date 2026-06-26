package outbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/worker"
)

// RelayConfig configures the Relay worker.
type RelayConfig struct {
	// Name labels the worker (default "outbox-relay").
	Name string
	// PollInterval is the idle tick rate (default 2s) — the wait after a tick
	// that found no work. The relay paces adaptively: after a full batch it
	// drains again immediately, so a backlog clears without waiting a full
	// interval. It also runs an immediate first tick on startup.
	PollInterval time.Duration
	// MaxPollInterval, when greater than PollInterval, makes the idle wait back
	// off geometrically up to this cap over consecutive empty ticks (a quiet
	// relay polls less often). 0 keeps the idle wait at PollInterval.
	MaxPollInterval time.Duration
	// BatchSize is the max events leased per tick (default 100).
	BatchSize int
	// PublishTimeout bounds each publish + its follow-up mark (default 5s).
	PublishTimeout time.Duration
	// Metrics, when set, records relay throughput (published/failed/latency).
	Metrics *Metrics
}

// NewRelay builds the relay as a worker.Loop. It wires the outbox FSM onto a
// worker.Processor: LeasePending is the Source, the broker.Publisher is the
// Handler, and MarkSent/MarkFailed are the Sink. Run several relays (here via
// worker.Group, or across replicas) for throughput — SKIP LOCKED splits work.
//
// The loop paces adaptively (NewPacedLoop): a full batch means a backlog likely
// remains, so it drains again at once; an empty batch idles for PollInterval
// (optionally backing off to MaxPollInterval). Delivery is at-least-once: if the
// process dies after a successful publish but before MarkSent, the sweeper
// returns the row to pending and the relay republishes it. Consumers dedupe on
// the CloudEvents id.
func NewRelay(log *logger.Logger, store Store, pub broker.Publisher, cfg RelayConfig) *worker.Loop {
	if cfg.Name == "" {
		cfg.Name = "outbox-relay"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.PublishTimeout <= 0 {
		cfg.PublishTimeout = 5 * time.Second
	}

	proc := worker.NewProcessor[Event](
		log.Slog(),
		relaySource{store},
		relayPublisher{pub: pub, m: cfg.Metrics},
		relaySink{store},
		worker.ProcessorConfig{
			Name:          cfg.Name,
			BatchSize:     cfg.BatchSize,
			HandleTimeout: cfg.PublishTimeout,
			// Terminality is decided by the store (attempts vs max_attempts) in
			// MarkFailed, not by the error, so the classifier is a no-op.
		},
	)
	return worker.NewPacedLoop(log.Slog(), worker.LoopConfig{
		Name:               cfg.Name,
		Interval:           cfg.PollInterval,
		MaxIdleInterval:    cfg.MaxPollInterval,
		ImmediateFirstTick: true,
	}, proc.PacedTick())
}

// relaySource adapts Store.LeasePending to worker.Source[Event].
type relaySource struct{ store Store }

func (s relaySource) Claim(ctx context.Context, now time.Time, limit int) ([]Event, error) {
	return s.store.LeasePending(ctx, now, limit)
}

// relayPublisher adapts a broker.Publisher to worker.Handler[Event], recording
// publish throughput/latency when Metrics is set.
type relayPublisher struct {
	pub broker.Publisher
	m   *Metrics
}

func (p relayPublisher) Handle(ctx context.Context, ev Event) error {
	start := time.Now()
	err := p.pub.Publish(ctx, broker.Message{
		ID:              ev.ID.String(),
		Type:            ev.Type,
		Time:            ev.CreatedAt,
		DataContentType: ev.ContentType,
		Data:            ev.Payload,
		Topic:           ev.Topic,
		Key:             ev.Key,
		Headers:         decodeHeaders(ev.Headers),
	})
	p.m.observePublish(time.Since(start), err)
	return err
}

// relaySink adapts Store mark methods to worker.Sink[Event]. The lease guard
// uses the event's own lease id (set by LeasePending); terminal is ignored
// because the store decides terminality from attempts vs max_attempts.
type relaySink struct{ store Store }

func (s relaySink) MarkDone(ctx context.Context, ev Event, now time.Time) error {
	return s.store.MarkSent(ctx, ev, ev.LeaseID, now)
}

func (s relaySink) MarkFailed(ctx context.Context, ev Event, errMsg string, _ bool, now time.Time) error {
	return s.store.MarkFailed(ctx, ev, ev.LeaseID, errMsg, now)
}

// SweeperConfig configures the Sweeper worker.
type SweeperConfig struct {
	Name string // default "outbox-sweeper"
	// Interval between sweeps (default 30s).
	Interval time.Duration
	// LeaseTimeout: in_flight rows leased longer than this are reclaimed
	// (default 1m). Set comfortably above RelayConfig.PublishTimeout.
	LeaseTimeout time.Duration
	// BatchSize bounds rows reclaimed per sweep (default 100).
	BatchSize int
	// Metrics, when set, counts reclaimed leases.
	Metrics *Metrics
}

// NewSweeper builds the lease-reclaim worker as a worker.Loop. It returns
// in_flight rows abandoned by a crashed relay back to pending.
func NewSweeper(log *logger.Logger, store Store, cfg SweeperConfig) *worker.Loop {
	if cfg.Name == "" {
		cfg.Name = "outbox-sweeper"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.LeaseTimeout <= 0 {
		cfg.LeaseTimeout = time.Minute
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	tick := func(ctx context.Context) error {
		n, err := store.SweepExpiredLeases(ctx, cfg.LeaseTimeout, time.Now().UTC(), cfg.BatchSize)
		cfg.Metrics.addSwept(n)
		return err
	}
	return worker.NewLoop(log.Slog(), worker.LoopConfig{Name: cfg.Name, Interval: cfg.Interval}, tick)
}

// CleanerConfig configures the Cleaner worker.
type CleanerConfig struct {
	Name string // default "outbox-cleaner"
	// Interval between cleanups (default 1h).
	Interval time.Duration
	// Retention: sent/failed rows older than this are deleted (default 24h).
	Retention time.Duration
	// Metrics, when set, counts deleted rows.
	Metrics *Metrics
}

// NewCleaner builds the retention worker as a worker.Loop. It deletes terminal
// rows past the retention window to keep the table small.
func NewCleaner(log *logger.Logger, store Store, cfg CleanerConfig) *worker.Loop {
	if cfg.Name == "" {
		cfg.Name = "outbox-cleaner"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Hour
	}
	if cfg.Retention <= 0 {
		cfg.Retention = 24 * time.Hour
	}
	tick := func(ctx context.Context) error {
		n, err := store.Cleanup(ctx, cfg.Retention, time.Now().UTC())
		cfg.Metrics.addCleaned(n)
		return err
	}
	return worker.NewLoop(log.Slog(), worker.LoopConfig{Name: cfg.Name, Interval: cfg.Interval}, tick)
}

// decodeHeaders parses the JSONB headers blob into a string map for transport,
// dropping non-string values. Returns nil for empty/invalid blobs.
func decodeHeaders(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
