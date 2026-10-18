// Package metrics is the one Prometheus registry of a process: the base
// preset, the HTTP server metrics and the scrape handler.
//
// # One registry
//
// The service builds one Metrics in the Deps literal, before the initializers,
// and passes it to every component that records metrics — SDK packages and the
// service's own code alike. Metrics embeds *prometheus.Registry, so m itself is
// the prometheus.Registerer a component takes. Nothing registers on
// prometheus.DefaultRegisterer.
//
//	d := &deps.Deps{Opts: opts, Logger: log, Metrics: metrics.New()}
//
//	r.Handle("GET /metrics", d.Metrics.Handler())
//	outbox.NewRelay(log, ob, pub, outbox.RelayConfig{Metrics: d.Metrics})
//
// New registers the base preset: go_*, process_* and go_build_info (the module
// path, version and checksum of the main module from the go build stamp).
//
// # Names
//
// SDK metrics are named skit_<subsystem>_<name>_<unit> whatever the service:
// services and pods differ by the target labels Prometheus adds (job,
// namespace, pod), so one dashboard serves every service. Durations are in
// seconds, counters end in _total. Label values come from closed sets only —
// a route template, a status code, a component name — never an id or an error
// text.
//
// # Presets
//
// Every SDK package records its own metrics on the registry its config gets;
// a nil registry turns them off:
//
//   - metrics: skit_http_server_* — m.Middleware().
//   - grpcserver: skit_grpc_server_handled_total by method and code,
//     skit_grpc_server_handling_seconds by method, skit_grpc_server_in_flight
//     — Config.Metrics.
//   - httpclient: skit_http_client_requests_total by client, method, status
//     code and error type, skit_http_client_request_duration_seconds by client
//     — Config.Name and Config.Metrics.
//   - rest/mid: skit_rest_outcomes_total by code — Config.Metrics.
//   - dbx: go_sql_* by db_name and host — Config.Metrics.
//   - outbox: skit_outbox_relay_*, skit_outbox_sweeper_reclaimed_total,
//     skit_outbox_cleaner_deleted_total — the configs of the workers; the
//     backlog gauges — NewBacklog.
//   - queue: skit_queue_handled_total by kind and result,
//     skit_queue_handle_duration_seconds by kind — WithMetrics; the backlog
//     gauges — NewBacklog.
//   - broker: skit_broker_consumed_total by consumer and action,
//     skit_broker_handle_duration_seconds by consumer — Instrument.
//   - worker: skit_worker_ticks_total by worker and result,
//     skit_worker_tick_duration_seconds and
//     skit_worker_last_success_timestamp_seconds by worker —
//     LoopConfig.Metrics.
//   - cron: skit_cron_runs_total by cron_job and result,
//     skit_cron_run_duration_seconds and
//     skit_cron_last_success_timestamp_seconds by cron_job — Config.Metrics.
//   - otel/setup: target_info and skit_otel_exported_spans_total by result —
//     Config.Metrics.
//
// # HTTP server
//
// Middleware records skit_http_server_requests_total by method, route and
// status code, skit_http_server_request_duration_seconds by method and route,
// and skit_http_server_requests_in_flight. The labels are named after the HTTP
// semantic conventions — http_request_method, http_route,
// http_response_status_code — as the span attributes are. Put it into the
// group of business routes, after middleware.Trace:
//
//	api := r.With(
//		middleware.Trace(middleware.TraceConfig{TracerProvider: d.TracerProvider}),
//		httplog.RequestLogger(accessLog, opts),
//		d.Metrics.Middleware(),
//	)
//
// # Exemplars
//
// The duration of a request whose span is sampled carries its trace_id and
// span_id as the exemplar. Handler serves OpenMetrics to a scraper that
// accepts it; Prometheus stores exemplars with
// --enable-feature=exemplar-storage.
//
// # Business metrics
//
// A domain package builds its collectors once, with promauto.With on the
// registry it gets in its Config; with a nil registry the collectors work but
// are not registered, so neither the core nor its tests check for nil. Name
// them <service>_<subsystem>_<name>_<unit>.
//
//	// core/order/metrics.go
//	func newMetrics(reg prometheus.Registerer) orderMetrics {
//		f := promauto.With(reg)
//		return orderMetrics{
//			created: f.NewCounterVec(prometheus.CounterOpts{
//				Name: "shop_order_created_total", Help: "Orders created.",
//			}, []string{"channel"}),
//		}
//	}
//
// # State by polling
//
// A gauge of state is read one of two ways, by what the read costs. A cheap
// read from memory runs on every scrape, in a GaugeFunc. An expensive or
// remote read — an SQL aggregate, another service — runs in a worker.Loop of
// the worker group, which sets the gauge; the skit_worker_* of the Loop show
// how fresh the value is. Every replica that reads a whole table reports the
// same value, so a query takes max without(pod, instance), not sum. The
// examples show both ways.
//
// # Dashboards
//
// The dashboards directory holds six Grafana dashboards of the presets:
// service (HTTP and gRPC servers, HTTP clients, REST outcomes, the Go runtime,
// the build and the health of the telemetry), dbx, outbox, queue, broker, and
// worker with cron. Their variables are datasource, namespace, job and pod and
// no service name is written in them, so one import serves every service. A
// test builds every preset on one registry and fails when a dashboard queries
// a metric that does not exist.
package metrics
