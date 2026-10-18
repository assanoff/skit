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
// # Usage
//
//	sched := cron.New(log, cron.Config{
//	    Name:           "reports",
//	    Metrics:        d.Metrics,
//	    TracerProvider: d.TracerProvider,
//	})
//	if err := sched.Add("0 3 * * *", "nightly-report", report.Run); err != nil {
//	    return err
//	}
//	group.Add(sched) // a worker.Runnable
//
// # Metrics
//
// With Config.Metrics set, every run is recorded under its job name:
// skit_cron_runs_total by cron_job and result (ok, error, panic),
// skit_cron_run_duration_seconds and skit_cron_last_success_timestamp_seconds
// by cron_job. The label is cron_job, not job: Prometheus puts its own job
// label on every series.
//
// # Tracing
//
// Every run is the root span of its own trace, named after the job and taken
// from Config.TracerProvider: a run has no request behind it, and what the job
// does — queries, calls, published events — nests under that span. A failed
// run sets the span's error; a panic sets error.type "panic".
//
// # Options
//
//   - Config: Name (default "cron"), Metrics, TracerProvider.
//   - WithOnPanic: a handler called when a job panics; the panic is recovered,
//     logged and counted either way.
package cron
