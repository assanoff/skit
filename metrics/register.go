package metrics

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// Register registers c on reg and returns it. If an identical collector is
// already registered (same fully-qualified name and labels) it returns the
// existing one instead of panicking; if a *different* collector clashes on the
// name it panics, surfacing the wiring bug at startup.
//
// This is the building block for conflict-free, extensible metrics: each
// package (SDK or application) defines its own collectors and registers them on
// the one shared registry through Register. Distinct namespaces/subsystems keep
// names from colliding; the register-or-get behavior makes constructing a
// subsystem's metrics idempotent, so building it twice against the same registry
// (e.g. in tests, or two workers sharing a registry) reuses the collectors
// rather than crashing.
//
// A nil reg disables registration: c is returned unregistered, so its methods
// (Inc/Observe/…) remain safe no-ops from the scrape's perspective. This lets a
// component accept an optional registry and stay metrics-agnostic.
func Register[C prometheus.Collector](reg prometheus.Registerer, c C) C {
	if reg == nil {
		return c
	}
	if err := reg.Register(c); err != nil {
		if already, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
			if existing, ok := already.ExistingCollector.(C); ok {
				return existing
			}
		}
		panic(err)
	}
	return c
}
