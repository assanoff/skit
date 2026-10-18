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
package metrics
