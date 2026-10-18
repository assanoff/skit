// Package cron runs jobs on a wall-clock schedule as a supervised
// worker.Runnable. It is a thin, production-shaped wrapper over robfig/cron: it
// validates specs up front, recovers panics so one bad run cannot crash the
// process, threads the run context into every job, and stops gracefully when
// its context is canceled.
//
// It complements the worker package: a worker.Loop fires on a fixed interval
// since boot ("every 5m"); a cron Scheduler fires on a wall-clock spec ("at
// 03:00 daily", "@hourly", "@every 1h30m"). For a job that must run on at most
// one replica, gate it with a lock.Locker (see `skit add cron --lock`).
//
// Specs accept the standard 5-field cron syntax (minute hour dom month dow) and
// the @-descriptors (@hourly, @daily, @weekly, @monthly, @every <dur>).
//
// With Config.Metrics set, every run is recorded under its job name:
// skit_cron_runs_total by cron_job and result (ok, error, panic),
// skit_cron_run_duration_seconds and skit_cron_last_success_timestamp_seconds
// by cron_job. The label is cron_job, not job: Prometheus puts its own job
// label on every series.
package cron

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	robfig "github.com/robfig/cron/v3"

	"github.com/assanoff/skit/internal/prom"
	"github.com/assanoff/skit/safetick"
)

// specParser accepts standard 5-field specs plus @-descriptors (including
// @every). It is used both to validate specs in Add and to drive the running
// scheduler, so validation and execution never disagree.
var specParser = robfig.NewParser(
	robfig.Minute | robfig.Hour | robfig.Dom | robfig.Month | robfig.Dow | robfig.Descriptor,
)

// Job is one scheduled unit of work. A returned error is logged; it does not
// stop the schedule. Jobs receive the scheduler's run context, so a long job
// should honor cancellation for a prompt shutdown.
type Job func(ctx context.Context) error

// entry is a validated (spec, job) pair awaiting Start.
type entry struct {
	spec string
	name string
	job  Job
	runs *prom.Runs
}

// Scheduler fires registered Jobs on their cron specs. Build it, Add jobs, then
// hand it to a worker.Group as a Runnable. It is not safe to Add after Start.
type Scheduler struct {
	name    string
	log     *slog.Logger
	metrics prometheus.Registerer
	onPanic safetick.PanicHandler
	entries []entry
}

// Config configures a Scheduler.
type Config struct {
	// Name identifies the scheduler in logs (defaults to "cron").
	Name string
	// Metrics is the registry of the run metrics of the jobs; nil disables
	// them.
	Metrics prometheus.Registerer
}

// Option customizes a Scheduler.
type Option func(*Scheduler)

// WithOnPanic sets a handler invoked when a job panics. The panic is always
// recovered, logged and counted regardless.
func WithOnPanic(h safetick.PanicHandler) Option {
	return func(s *Scheduler) { s.onPanic = h }
}

// New builds an empty Scheduler. log may be nil.
func New(log *slog.Logger, cfg Config, opts ...Option) *Scheduler {
	s := &Scheduler{name: cmp.Or(cfg.Name, "cron"), log: log, metrics: cfg.Metrics}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Add registers job to run on spec. The spec is validated immediately, so a
// typo fails at wiring time rather than silently never firing. jobName labels
// the job in logs and metrics; give each job its own.
func (s *Scheduler) Add(spec, jobName string, job Job) error {
	if job == nil {
		return fmt.Errorf("cron %q: nil job for %q", s.name, jobName)
	}
	if _, err := specParser.Parse(spec); err != nil {
		return fmt.Errorf("cron %q: invalid spec %q for %q: %w", s.name, spec, jobName, err)
	}
	s.entries = append(s.entries, entry{
		spec: spec,
		name: jobName,
		job:  job,
		runs: prom.NewRuns(s.metrics, prom.RunsOpts{
			Subsystem: "cron", Run: "run", Label: "cron_job",
		}, jobName),
	})
	return nil
}

// Name implements worker.Runnable.
func (s *Scheduler) Name() string { return s.name }

// Start implements worker.Runnable: it runs the scheduler until ctx is canceled,
// then waits for any in-flight jobs to finish before returning. Each firing runs
// in its own goroutine (robfig default), wrapped in panic recovery and given
// ctx.
func (s *Scheduler) Start(ctx context.Context) error {
	c := robfig.New(robfig.WithParser(specParser))

	for _, e := range s.entries {
		if _, err := c.AddFunc(e.spec, func() { s.run(ctx, e) }); err != nil {
			// Specs were validated in Add, so this is unexpected — surface it.
			return fmt.Errorf("cron %q: schedule %q: %w", s.name, e.name, err)
		}
	}

	c.Start()
	<-ctx.Done()

	// Stop accepting new runs and wait for running jobs to drain.
	<-c.Stop().Done()
	return nil
}

// Stop implements worker.Runnable. The scheduler stops when its Start context is
// canceled (the worker.Group cancels it on shutdown), so Stop is a no-op.
func (s *Scheduler) Stop(context.Context) error { return nil }

// run executes one job firing under panic recovery and records it, skipping
// it if the context is already canceled (shutdown in progress).
func (s *Scheduler) run(ctx context.Context, e entry) {
	if ctx.Err() != nil {
		return
	}
	started := time.Now()
	var err error
	// finished stays false when the job panics: RecoverTick, deferred last,
	// runs first and stops the panic, then Record counts the run as a panic.
	finished := false
	defer func() { e.runs.Record(ctx, time.Since(started), finished, err) }()
	defer safetick.RecoverTick(ctx, s.log, s.name+"/"+e.name, s.onPanic)

	err = e.job(ctx)
	finished = true
	if err != nil && s.log != nil {
		s.log.ErrorContext(ctx, "cron job failed",
			"cron", s.name, "job", e.name, "err", err,
			"elapsed", time.Since(started).String(),
		)
	}
}
