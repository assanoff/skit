// Package prom holds the Prometheus helpers the SDK packages share: register
// or get the collector already registered (MustRegister), the bucket bounds
// of durations (DurationBuckets), a duration observed with the trace of its
// context as the exemplar (ObserveDuration), and the metrics of the runs of
// periodic work (Runs), which worker and cron record.
//
//	reqs := prom.MustRegister(cfg.Metrics, prometheus.NewCounterVec(opts, labels))
//	prom.ObserveDuration(ctx, hist.WithLabelValues(name), time.Since(start))
//
// A nil registry turns registration off: the collector is returned
// unregistered and stays safe to use.
package prom
