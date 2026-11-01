package queue

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/assanoff/skit/retry"
	"github.com/assanoff/skit/safetick"
	"github.com/assanoff/skit/worker"
)

// Config configures the workers of one queue.
type Config struct {
	// Queue names the queue the workers serve; required.
	Queue string
	// Concurrency is the number of claim loops per process (default 1).
	Concurrency int
	// Batch is the number of tasks a loop claims at once (default 1).
	Batch int
	// Timeout bounds the handler of one task (default 30s). A batch starts no
	// task once it has run Timeout, and a task stays leased for 3 × Timeout +
	// 10s: after that, a task of a crashed worker goes to another.
	Timeout time.Duration
	// Poll is the wait after a claim that found no task, or after a batch
	// stopped by Halt (default 1s). After a full batch the loop claims again at
	// once.
	Poll time.Duration
}

func (c Config) withDefaults() Config {
	c.Concurrency = cmp.Or(c.Concurrency, 1)
	c.Batch = cmp.Or(c.Batch, 1)
	c.Timeout = cmp.Or(c.Timeout, 30*time.Second)
	c.Poll = cmp.Or(c.Poll, time.Second)
	return c
}

// lease is how long a claimed task stays leased.
func (c Config) lease() time.Duration {
	return 3*c.Timeout + 10*time.Second
}

const (
	// deadRetention is how long a dead task stays in the table for
	// inspection before the cleaner removes it.
	deadRetention = 7 * 24 * time.Hour
	// cleanInterval is how often the cleaner of a queue runs.
	cleanInterval = time.Hour
)

// Option customizes the workers of a queue.
type Option func(*options)

// options collects the options of Workers.
type options struct {
	backoff retry.Backoff
	onPanic func(ctx context.Context, t Task, p *safetick.PanicError) error
	metrics prometheus.Registerer
}

// WithBackoff sets the delay before a failed task runs again; a zero field
// keeps its default: Base 2s, Factor 2, Max 5m. MaxAttempts 0, the default,
// retries without limit; a positive MaxAttempts sends the task to the dead
// letters at that many failed attempts. Jitter is not applied.
func WithBackoff(b retry.Backoff) Option {
	return func(o *options) {
		o.backoff = b
	}
}

// WithOnPanic sets what a panic in the handler means for its task: the panic
// is recovered and logged with its stack, then fn returns the outcome — nil
// removes the task, Permanent dead-letters it, Halt stops the batch, any
// other error retries the task. Without it the task is retried.
func WithOnPanic(fn func(ctx context.Context, t Task, p *safetick.PanicError) error) Option {
	return func(o *options) {
		o.onPanic = fn
	}
}

// WithMetrics records the metrics of the queue on reg, labeled with the queue:
//
//	skit_queue_handled_total{queue,kind,result}  — tasks by result: ok or error
//	skit_queue_handle_duration_seconds{queue,kind} — duration of the handler
//	skit_queue_pending{queue}, skit_queue_leased{queue}, skit_queue_dead{queue}
//
// and the tick metrics of the loops, skit_worker_*, under the loop names
// queue-<queue>, queue-<queue>-cleaner and queue-<queue>-backlog. The
// backlog gauges come from a loop of their own that Workers adds only with
// WithMetrics. A panic in the handler counts as an error.
func WithMetrics(reg prometheus.Registerer) Option {
	return func(o *options) {
		o.metrics = reg
	}
}

// Workers builds the workers of the queue cfg.Queue: Concurrency loops that
// claim its tasks and run h on each, the cleaner of its dead letters (kept 7
// days) and, with WithMetrics, the loop of its backlog gauges. Supervise them
// in a worker.Group; run them on every replica — the claim splits the tasks.
//
// The error of h decides the outcome of a task (see JobFunc). A failed task
// runs again after a backoff (WithBackoff), and its Attempts count only the
// failures: a task released from a stopped batch, or reclaimed after a
// crash, keeps them. Workers returns an error on an empty cfg.Queue or a nil
// h.
func Workers(log *slog.Logger, q *PG, cfg Config, h JobFunc, opts ...Option) ([]worker.Runnable, error) {
	switch {
	case cfg.Queue == "":
		return nil, errors.New("queue workers: queue is required")
	case h == nil:
		return nil, errors.New("queue workers: handler is nil")
	}
	cfg = cfg.withDefaults()
	o := newOptions(opts)

	proc := newProcessor(log, q, cfg, h, o)
	loop := worker.LoopConfig{
		Name:               "queue-" + cfg.Queue,
		Interval:           cfg.Poll,
		ImmediateFirstTick: true,
		Metrics:            o.metrics,
	}

	out := make([]worker.Runnable, 0, cfg.Concurrency+2)
	for range cfg.Concurrency {
		out = append(out, worker.NewPacedLoop(log, loop, proc.PacedTick()))
	}
	out = append(out, newCleaner(log, q, cfg.Queue, o.metrics))
	if o.metrics != nil {
		out = append(out, newBacklogLoop(log, q, cfg.Queue, o.metrics))
	}
	return out, nil
}

// newProcessor builds the processor that claims, handles and marks the tasks
// of the queue cfg.Queue.
func newProcessor(log *slog.Logger, q *PG, cfg Config, h JobFunc, o options) *worker.Processor[leased] {
	c := &consumer{
		q:       q,
		cfg:     cfg,
		h:       h,
		backoff: o.backoff,
		metrics: newTaskMetrics(o.metrics, cfg.Queue),
	}
	return worker.NewProcessor[leased](log, c, c, c, worker.ProcessorConfig{
		Name:          "queue-" + cfg.Queue,
		BatchSize:     cfg.Batch,
		HandleTimeout: cfg.Timeout,
		BatchTimeout:  cfg.Timeout,
		IsTerminal:    isPermanent,
		IsHalt:        isHalt,
		OnPanic:       o.processorOnPanic(),
	})
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	o.backoff.Base = cmp.Or(o.backoff.Base, 2*time.Second)
	o.backoff.Factor = cmp.Or(o.backoff.Factor, 2)
	o.backoff.Max = cmp.Or(o.backoff.Max, 5*time.Minute)
	return o
}

// processorOnPanic adapts the WithOnPanic handler to the item of the
// processor; nil keeps the default of the processor, a retry.
func (o options) processorOnPanic() func(context.Context, any, *safetick.PanicError) error {
	if o.onPanic == nil {
		return nil
	}
	return func(ctx context.Context, item any, p *safetick.PanicError) error {
		t, ok := item.(leased)
		if !ok {
			return p
		}
		return o.onPanic(ctx, t.Task, p)
	}
}

// newCleaner builds the loop that removes the dead tasks of queue older than
// deadRetention, once an hour.
func newCleaner(log *slog.Logger, q *PG, queue string, reg prometheus.Registerer) *worker.Loop {
	tick := func(ctx context.Context) error {
		_, err := q.cleanup(ctx, queue, time.Now().UTC().Add(-deadRetention))
		return err
	}
	return worker.NewLoop(log, worker.LoopConfig{
		Name:     "queue-" + queue + "-cleaner",
		Interval: cleanInterval,
		Metrics:  reg,
	}, tick)
}

// consumer is the source, handler and sink of the processor of one queue.
type consumer struct {
	q       *PG
	cfg     Config
	h       JobFunc
	backoff retry.Backoff
	metrics *taskMetrics
}

// Claim implements worker.Source.
func (c *consumer) Claim(ctx context.Context, now time.Time, limit int) ([]leased, error) {
	return c.q.claim(ctx, c.cfg.Queue, now, c.cfg.lease(), limit)
}

// Handle implements worker.Handler: it runs the handler of the queue and
// records the metrics of the task. A panic is returned as a
// *safetick.PanicError, which the processor logs and passes to OnPanic.
func (c *consumer) Handle(ctx context.Context, t leased) error {
	start := time.Now()
	err := safetick.Call(func() error { return c.h(ctx, t.Task) })
	c.metrics.observe(ctx, t.Kind, time.Since(start), err)
	return err
}

// MarkDone implements worker.Sink: a handled task is removed.
func (c *consumer) MarkDone(ctx context.Context, t leased, _ time.Time) error {
	return c.q.markDone(ctx, t)
}

// MarkFailed implements worker.Sink: the attempt counts, and the task dies
// when the error is terminal or the attempts are spent; otherwise it runs
// again after the backoff of its attempts.
func (c *consumer) MarkFailed(ctx context.Context, t leased, errMsg string, terminal bool, now time.Time) error {
	attempts := t.Attempts + 1
	dead := terminal || c.backoff.Exhausted(attempts)
	return c.q.markFailed(ctx, t, errMsg, dead, now, now.Add(c.backoff.Next(attempts)))
}

// Release implements worker.Releaser: the rest of a stopped batch goes back.
func (c *consumer) Release(ctx context.Context, rest []leased) error {
	return c.q.release(ctx, rest)
}
