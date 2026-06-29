// Package queue is a durable work queue with at-least-once delivery, safe for
// concurrent consumers across processes.
//
// The Postgres implementation (PG) claims work with SELECT … FOR UPDATE SKIP
// LOCKED, so running N consumers (goroutines or replicas) over one table just
// works — each ready task goes to exactly one consumer at a time. A Queue plugs
// straight into worker.Processor: its Claim/MarkDone/MarkFailed methods satisfy
// worker.Source[Task] and worker.Sink[Task], so the reliable
// claim -> handle -> ack/retry loop comes for free. Schedule enqueues work; an
// optional unique Name deduplicates retries of the same logical task. An InMem
// implementation with identical semantics is provided for unit tests and local
// runs without a database.
//
// # Wiring (Postgres + worker.Processor)
//
// Schedule produces work; a worker.Processor consumes it by leasing a batch
// (Claim), running a Handler, and recording the outcome (MarkDone /
// MarkFailed):
//
//	q := queue.NewPG(log, db, queue.Options{LeaseTimeout: 5 * time.Minute})
//	if err := q.EnsureSchema(ctx); err != nil { // tests; in prod run Schema() as a migration
//	    return err
//	}
//
//	// Producer: enqueue a task. A Name makes the enqueue idempotent.
//	_, err := q.Schedule(ctx, queue.ScheduleParams{
//	    Name:    "send-welcome:42",
//	    Kind:    "email.welcome",
//	    Payload: payload,
//	    Delay:   0,
//	})
//
//	// Consumer: a Mux routes each task to a handler by Kind, so one queue and
//	// one worker pool can serve many task kinds. q is both the Source (Claim)
//	// and Sink (MarkDone/MarkFailed).
//	mux := queue.NewMux()
//	if err := mux.Register("email.welcome", sendWelcome); err != nil {
//	    return err
//	}
//	proc := worker.NewProcessor[queue.Task](log, q, mux, q, worker.ProcessorConfig{
//	    Name:       "queue",
//	    BatchSize:  100,
//	    IsTerminal: func(err error) bool { return errors.Is(err, queue.ErrUnknownKind) },
//	})
//
//	group := worker.NewGroup(log, 10*time.Second)
//	group.Add(worker.NewPacedLoop(log, worker.LoopConfig{}, proc.PacedTick()))
//	if err := group.Run(ctx); err != nil {
//	    return err
//	}
//
// # Dispatch and retry
//
// Mux maps a Task.Kind to a JobFunc and implements worker.Handler[Task]
// (Register returns an error — it never panics — on an empty kind, nil handler,
// or duplicate). Because Claim does not filter by Kind, the Mux is what makes
// the Kind column route work. Retry wraps a JobFunc with in-process retry (over
// the retry package) to absorb transient errors quickly; for durable backoff
// that survives crashes and frees the worker between attempts, rely instead on
// the store retry below (a non-terminal MarkFailed reschedules the task).
//
// # Schema
//
// Schema returns the DDL for the fixed queue_tasks table and its ready-task
// index; embed it in a migration. PG.EnsureSchema runs the same DDL directly
// and is convenient in tests.
//
// # Semantics
//
// Claim leases up to limit ready tasks (run_at <= now and not currently leased,
// or with an expired lease), bumps each task's attempt count, and stamps a
// fresh LeaseID that MarkDone/MarkFailed must echo back. MarkDone acknowledges
// success and removes the task. MarkFailed with terminal=false releases the
// lease and reschedules the task to now+RetryDelay so a later Claim retries it
// without busy-looping; terminal=true parks it as a dead letter. When a lease
// no longer matches — because it expired and another
// consumer reclaimed the task — the mark is a no-op and returns ErrLeaseLost.
// PG.Cleanup reaps done/dead-lettered rows finished before a cutoff; run it
// periodically via worker.Loop.
//
// # Options
//
//   - Options.LeaseTimeout: how long a claimed task stays leased before another
//     consumer may reclaim it (default 5m).
//   - Options.RetryDelay: how long a retryable (non-terminal) failure waits
//     before it is claimable again (default 30s). NewInMem takes the same
//     Options as NewPG.
//   - ScheduleParams: Name (optional dedup key — empty means always insert),
//     Kind (handler routing key, required), Payload (opaque body), Delay
//     (postpones the earliest claim time).
//
// The backing table is fixed (queue_tasks); all time math and lease-id
// generation happen in Go, keeping the SQL free of NOW()/gen_random_uuid().
package queue
