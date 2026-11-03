// Package worker runs the background work of a service: supervised
// components, recurring loops, bounded fan-out and claim-based batches.
//
// Instead of every service hand-rolling its own run loops and supervisors,
// worker provides a handful of composable pieces:
//
//   - Runnable: anything the app supervises (HTTP/gRPC servers, consumers,
//     loops). RunnableFunc adapts a plain start function into one.
//   - Group: a supervisor that starts Runnables concurrently and shuts them
//     down together (LIFO), tearing the whole group down on the first failure.
//   - Loop: a recurring tick loop with panic recovery — fixed-interval
//     (NewLoop) or adaptively paced (NewPacedLoop).
//   - Pool: bounded-concurrency fan-out of one-shot jobs.
//   - Processor[T]: a reliable claim -> handle -> ack/retry batch pipeline,
//     built from Steps (functions) and run as a Loop.
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
// A Processor[T] is the reliable batch-processing FSM behind the queue workers
// and other claim-based consumers. Its Steps are functions: each tick Claim
// leases a batch (canonically SELECT ... FOR UPDATE SKIP LOCKED, so replicas
// can run concurrently), Handle runs on each item, then MarkDone or MarkFailed
// records the outcome (terminal or retryable, as IsTerminal decides). An error
// IsHalt accepts stops the batch, and Release, when set, returns the rest to
// the source untouched: a dependency that is down costs one failed item per
// tick, not a batch. A canceled tick and BatchTimeout stop the batch before
// its next item and release the rest the same way, so no item is handled past
// the lease of its source. Marks and releases run without the cancellation of
// the tick, each under its own timeout: an item handled at shutdown is still
// acknowledged. A panic in Handle is recovered per item and logged with its
// stack; by default the item is retried, and OnPanic can decide otherwise.
// Claim, Handle, MarkDone and MarkFailed are required: NewProcessor panics
// when one is nil. In-process retry of an item is retry.Do inside Handle. A
// Processor is not itself a Runnable: call Tick (or PacedTick) to get a tick
// function and wrap it in a Loop.
//
// # Usage
//
// Wire a Processor into a Loop and supervise it (plus any other Runnables) in
// a Group:
//
//	steps := worker.Steps[Job]{
//	    Claim:      store.Claim,
//	    Handle:     handle,
//	    MarkDone:   store.MarkDone,
//	    MarkFailed: store.MarkFailed,
//	    Release:    store.Release, // optional
//	}
//	proc := worker.NewProcessor(log, steps, worker.ProcessorConfig{
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
//	    log.ErrorContext(ctx, "group stopped", "err", err)
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
// # Tracing
//
// A tick has no request behind it, so by default a Loop runs it under
// otel.Suppress: the queries of an idle poll export no traces. A Processor
// lifts the mark for each item's handler, which then sets the item's trace —
// the queue of events continues the trace that created the event — while the
// claim and the marks stay suppressed. With LoopConfig.Trace a Loop records
// every tick as the root span of its own trace, named after the Loop, and
// everything the tick does nests under it; a failed tick sets the span's
// error, a panic sets error.type "panic".
//
// # Config
//
// LoopConfig: Name (default "worker"), Interval (default 1s; the idle interval
// for an adaptive loop), ImmediateFirstTick, TickTimeout, Metrics, Trace,
// TracerProvider (nil → the global provider), and the adaptive fields
// BusyInterval and MaxIdleInterval. Option WithOnPanic sets a handler invoked
// when a tick panics.
//
// ProcessorConfig: Name (default "processor"), BatchSize (default 100),
// HandleTimeout, BatchTimeout, IsTerminal (nil = all failures retryable),
// IsHalt (nil = no error stops the batch), OnPanic (nil = a panicked item is
// retried), Now (default time.Now().UTC; override in tests).
//
// GroupConfig: ShutdownTimeout (0 = no bound on how long Run waits for
// Runnables to Stop).
//
// PoolConfig: MaxConcurrent (default 1).
package worker
