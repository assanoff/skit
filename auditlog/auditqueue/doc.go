// Package auditqueue is the durable auditlog.Recorder: Record schedules the
// audit entry as a task of a skit queue — one INSERT on the request path — and
// the workers of the queue write it through the audit core. Use it when an
// entry must survive a crash or the writes come in spikes; for the lowest
// latency and the order of each model use auditlog.AsyncRecorder.
//
// # Wiring
//
// The recorder schedules on the queue, the workers drain it into the core;
// supervise them in a worker.Group:
//
//	q := queue.NewPG(log, db)
//	if err := q.EnsureSchema(ctx); err != nil {
//	    return err
//	}
//	rec := auditqueue.NewRecorder(q, log) // an auditlog.Recorder
//	auditbus.Register(bus, rec)
//
//	workers, err := auditqueue.NewWorkers(q, core, log, auditqueue.WorkerConfig{
//	    Metrics: reg,
//	})
//	if err != nil {
//	    return err
//	}
//	for _, w := range workers {
//	    group.Add(w)
//	}
//
// The tasks go to the queue Queue under the kind Kind. Record is
// best-effort: a failed schedule is logged, not returned. A task whose
// payload does not decode goes to the dead letters of the queue; a failed
// write is retried with backoff.
//
// # WorkerConfig
//
//   - Interval: the wait after a poll that found no entry (default 1s).
//   - Batch: the entries claimed at once (default 100).
//   - Metrics: the registry of the skit_queue_* and skit_worker_* metrics of
//     the queue; nil turns them off.
//
// # Order
//
// The queue keeps no order within a model: the workers of every replica claim
// its tasks. Core.Create retries on a version collision, so no entry is lost,
// but two changes of one model handled out of order are stored in that order.
// Run the workers on one replica, or use AsyncRecorder, when the order
// matters.
package auditqueue
