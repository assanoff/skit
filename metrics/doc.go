// Package metrics provides a Prometheus registry, a scrape handler, an HTTP
// request middleware, and the building block for extensible, conflict-free
// metrics across the SDK and the application.
//
// # One shared registry
//
// The model is a single registry, dependency-injected. The application builds
// one Metrics (which owns a *prometheus.Registry) at startup and passes that
// registry to every component that exposes metrics — SDK subsystems and the
// application's own business metrics alike. One registry means one /metrics
// endpoint and one place series can collide, so collisions are caught at
// startup rather than going silently unscraped.
//
//	m := metrics.New("myapp")        // app-wide registry + HTTP collectors
//	http.Handle("/metrics", m.Handler())
//
// # Each package owns its metrics
//
// A subsystem defines its own collectors next to its code and registers them on
// the injected registry. It never reaches for the global default registry, so
// importing it has no global side effects and two subsystems never fight over
// registration. The outbox package is the model: outbox.NewMetrics(reg) builds
// skit_outbox_* collectors; the application just hands it m.Registry.
//
//	om := outbox.NewMetrics(m.Registry)               // SDK subsystem metrics
//	relay := outbox.NewRelay(log, store, pub, outbox.RelayConfig{Metrics: om})
//	m.Registry.MustRegister(outbox.NewBacklogCollector(store, log)) // a custom collector
//
// # Adding your own metrics
//
// Application metrics use the same registry; distinct namespaces/subsystems
// keep names from colliding with the SDK's:
//
//	ordersTotal := metrics.Register(m.Registry, prometheus.NewCounterVec(
//	    prometheus.CounterOpts{Namespace: "myapp", Subsystem: "orders", Name: "created_total"},
//	    []string{"channel"}))
//	ordersTotal.WithLabelValues("web").Inc()
//
// # Conflict-free registration
//
// Register is the primitive that makes this safe and ergonomic: it registers a
// collector and returns it, returns the already-registered equivalent instead
// of panicking on a duplicate (so constructing a subsystem's metrics twice — in
// tests, or across workers sharing a registry — is idempotent), and panics only
// on a genuine name clash between *different* collectors, surfacing the wiring
// bug at startup. A nil registry disables registration while leaving the
// collector's methods as safe no-ops, so a component can take an optional
// registry and stay metrics-agnostic.
//
// # Naming
//
// Use Namespace + Subsystem + Name on every collector. Convention here: the SDK
// uses the "skit" namespace with a per-subsystem Subsystem (e.g.
// skit_outbox_*); the application uses its own namespace. Distinct
// (namespace, subsystem, name) tuples are what guarantee no conflicts on the
// shared registry.
package metrics
