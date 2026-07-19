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
package cron

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	robfig "github.com/robfig/cron/v3"

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
}

// Scheduler fires registered Jobs on their cron specs. Build it, Add jobs, then
// hand it to a worker.Group as a Runnable. It is not safe to Add after Start.
type Scheduler struct {
	name    string
	log     *slog.Logger
	onPanic safetick.PanicHandler
	entries []entry
}

// New builds an empty Scheduler named name (used in logs and panic metrics).
// log may be nil.
func New(log *slog.Logger, name string) *Scheduler {
	if name == "" {
		name = "cron"
	}
	return &Scheduler{name: name, log: log}
}

// OnPanic sets a handler invoked when a job panics (e.g. to bump a metric). The
// panic is always recovered and logged regardless.
func (s *Scheduler) OnPanic(h safetick.PanicHandler) *Scheduler {
	s.onPanic = h
	return s
}

// Add registers job to run on spec. The spec is validated immediately, so a
// typo fails at wiring time rather than silently never firing. jobName labels
// the job in logs.
func (s *Scheduler) Add(spec, jobName string, job Job) error {
	if job == nil {
		return fmt.Errorf("cron %q: nil job for %q", s.name, jobName)
	}
	if _, err := specParser.Parse(spec); err != nil {
		return fmt.Errorf("cron %q: invalid spec %q for %q: %w", s.name, spec, jobName, err)
	}
	s.entries = append(s.entries, entry{spec: spec, name: jobName, job: job})
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
		e := e
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

// run executes one job firing under panic recovery, skipping it if the context
// is already canceled (shutdown in progress).
func (s *Scheduler) run(ctx context.Context, e entry) {
	if ctx.Err() != nil {
		return
	}
	defer safetick.RecoverTick(s.log, s.name+"/"+e.name, s.onPanic)

	started := time.Now()
	if err := e.job(ctx); err != nil && s.log != nil {
		s.log.Error("cron job failed",
			"cron", s.name, "job", e.name, "error", err,
			"elapsed", time.Since(started).String(),
		)
	}
}
