// Package worker unifies the background-execution patterns found across the
// reference services into one small vocabulary.
//
// Instead of every service hand-rolling its own run loops, supervisors, and
// retry math, worker provides a handful of composable pieces:
//
//   - Runnable: anything the app supervises (HTTP/gRPC servers, consumers,
//     loops). RunnableFunc adapts a plain start function into one.
//   - Group: a supervisor that starts Runnables concurrently and shuts them
//     down together (LIFO), tearing the whole group down on the first failure.
//   - Loop: a recurring tick loop with panic recovery — fixed-interval
//     (NewLoop) or adaptively paced (NewPacedLoop).
//   - Pool: bounded-concurrency fan-out of one-shot jobs.
//   - Processor[T]: a reliable claim -> handle -> ack/retry batch pipeline,
//     built from a Source, Handler, and Sink, and run as a Loop.
//   - Retry[T]: wraps a Handler with in-process retry (over the retry package)
//     so transient failures are absorbed before the Sink records the outcome.
//
// # Loops and pacing
//
// A Loop runs a tick on a schedule, each tick wrapped in panic recovery
// (safetick) so one bad tick cannot crash the process. A fixed Loop ticks
// every Interval. An adaptive Loop (NewPacedLoop) varies its delay from the
// Pace its tick reports: Busy (a full batch — drain again after BusyInterval,
// or immediately) versus Idle (no work — wait Interval, optionally backing off
// geometrically toward MaxIdleInterval).
//
// # Processors
//
// A Processor[T] is the reliable batch-processing FSM behind the outbox relay
// and pgqueue-style consumers. Each tick it leases a batch from a Source
// (canonically SELECT ... FOR UPDATE SKIP LOCKED, so replicas can run
// concurrently), runs the Handler on each item, then records the outcome via
// the Sink (MarkDone on success, MarkFailed on error with terminal/retryable
// decided by IsTerminal). A Processor is not itself a Runnable: call Tick (or
// PacedTick) to get a tick function and wrap it in a Loop.
//
// # Usage
//
// Wire a Processor into a Loop and supervise it (plus any other Runnables) in
// a Group:
//
//	proc := worker.NewProcessor[Job](log, src, handler, sink, worker.ProcessorConfig{
//	    Name:       "jobs",
//	    BatchSize:  200,
//	    IsTerminal: func(err error) bool {
//	        return errors.Is(err, ErrBadInput)
//	    },
//	})
//	loop := worker.NewPacedLoop(log, worker.LoopConfig{
//	    Name:            "jobs",
//	    Interval:        time.Second, // idle wait
//	    BusyInterval:    0,           // drain backlog immediately
//	    MaxIdleInterval: 30 * time.Second,
//	}, proc.PacedTick())
//
//	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
//	g.Add(loop)
//	g.Add(worker.RunnableFunc{
//	    NameValue: "http",
//	    StartFn:   func(ctx context.Context) error {
//	        return srv.ListenAndServe()
//	    },
//	    StopFn:    func(ctx context.Context) error {
//	        return srv.Shutdown(ctx)
//	    },
//	})
//	if err := g.Run(ctx); err != nil {
//	    log.Error("group stopped", "err", err)
//	}
//
// For discrete fan-out work, submit jobs to a Pool:
//
//	pool := worker.NewPool(worker.PoolConfig{MaxConcurrent: 8})
//	key, err := pool.Submit(ctx, func(ctx context.Context) { do(ctx) })
//	_ = key // pool.Cancel(key) stops that one job
//	defer pool.Shutdown(context.Background())
//
// # Metrics
//
// With LoopConfig.Metrics set, a Loop records every tick under its Name:
//   - skit_worker_ticks_total by worker and result (ok, error, panic);
//   - skit_worker_tick_duration_seconds by worker;
//   - skit_worker_last_success_timestamp_seconds by worker, from the first
//     tick that ended without an error.
//
// A Processor is counted by the ticks of the Loop it runs in.
//
// # Config
//
// LoopConfig: Name (default "worker"), Interval (default 1s; the idle interval
// for an adaptive loop), ImmediateFirstTick, TickTimeout, Metrics, and the
// adaptive fields BusyInterval and MaxIdleInterval. Option WithOnPanic sets a
// handler invoked when a tick panics.
//
// ProcessorConfig: Name (default "processor"), BatchSize (default 100),
// HandleTimeout, IsTerminal (nil = all failures retryable), Now (default
// time.Now().UTC; override in tests).
//
// Retry wraps a Handler using retry.Config (Backoff + IsTerminal); the backoff
// policy itself now lives in the retry package.
//
// GroupConfig: ShutdownTimeout (0 = no bound on how long Run waits for
// Runnables to Stop).
//
// PoolConfig: MaxConcurrent (default 1).
package worker
