// Package queue runs durable background work with at-least-once delivery,
// safe for concurrent workers across goroutines and replicas.
//
// Every queue lives in one Postgres table, queue_tasks; the queue column
// tells them apart. A queue has its own workers, lease and backoff: give slow
// and fast work queues of their own, so a long task does not hold up a quick
// one. Within a queue, Kind selects the handler: a Mux routes each task to the
// JobFunc registered for its kind.
//
// # Usage
//
//	q := queue.NewPG(log, db)
//	if err := q.EnsureSchema(ctx); err != nil { // safe at startup, even across replicas
//	    return err
//	}
//
//	// Producer: a Key makes the enqueue idempotent within the queue.
//	_, err := q.Schedule(ctx, queue.Params{
//	    Queue:   "email",
//	    Kind:    "email.welcome",
//	    Key:     "welcome:42",
//	    Payload: payload,
//	})
//
//	// Consumer: the workers of the queue claim its tasks and run the Mux.
//	mux := queue.NewMux()
//	if err := mux.Register("email.welcome", queue.Decode(sendWelcome)); err != nil {
//	    return err
//	}
//	workers, err := queue.Workers(log, q, queue.Config{Queue: "email", Batch: 10},
//	    mux.Handle, queue.WithMetrics(reg))
//	if err != nil {
//	    return err
//	}
//	for _, w := range workers {
//	    group.Add(w) // a worker.Group
//	}
//
// # Outcomes
//
// The error of the handler decides the outcome of a task:
//
//   - nil removes the task;
//   - Permanent(err) sends it to the dead letters of its queue: a payload that
//     does not decode (Decode does this itself), a kind with no handler
//     (ErrUnknownKind), anything no retry fixes;
//   - Halt(err) retries it with backoff and stops the batch: the rest goes back
//     untouched, and the queue polls again after Config.Poll — for a
//     dependency every task needs, such as a broker that is down;
//   - any other error retries the task with backoff.
//
// A retried task runs again after a backoff of 2s × 2 up to 5 minutes, with
// no limit on attempts; WithBackoff changes it, and its MaxAttempts sends the
// task to the dead letters at that many failures. Task.Attempts counts only
// the failures: a task released from a stopped batch, or reclaimed after a
// crash, keeps its attempts. A panic in the handler is recovered and logged
// with its stack and, by default, retried; WithOnPanic decides otherwise.
//
// # Delivery
//
// A claim leases a batch of ready tasks of one queue with SELECT … FOR UPDATE
// SKIP LOCKED and returns it in run_at order. A task stays leased for
// 3 × Config.Timeout + 10s: if its worker dies, another reclaims it after
// that, so delivery is at-least-once and handlers must be idempotent. A batch
// starts no task once it has run Config.Timeout; at shutdown the task being
// handled is still marked and the rest goes back. A handled task is removed
// at once; a dead one stays 7 days for inspection, then the cleaner of its
// queue removes it.
//
// # Metrics
//
// With WithMetrics the workers of a queue record, labeled with the queue,
// skit_queue_handled_total by kind and result (ok, error — a panic is an
// error), skit_queue_handle_duration_seconds by kind and, from a loop of their
// own every 30 seconds, the backlog gauges skit_queue_pending,
// skit_queue_leased and skit_queue_dead. Every replica reports the same
// backlog, so a dashboard takes max, not sum. The loops record skit_worker_*
// under the names queue-<queue>, queue-<queue>-cleaner and
// queue-<queue>-backlog. The queue opens no spans.
//
// # Config and options
//
//   - Config: Queue (required), Concurrency (claim loops per process, default
//     1), Batch (tasks per claim, default 1), Timeout (per task, default 30s),
//     Poll (wait after an empty claim, default 1s).
//   - WithBackoff(retry.Backoff): the retry delay and the attempt limit.
//   - WithOnPanic(fn): the outcome of a task whose handler panicked.
//   - WithMetrics(reg): the registry of the metrics.
//   - WithFallback(fn): the Mux handler of a kind with no registered handler.
//   - Params: Queue and Kind (required), Key (dedup within the queue while
//     the task is in it — waiting, leased or dead), Payload, Delay.
package queue
