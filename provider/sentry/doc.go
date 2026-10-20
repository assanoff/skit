// Package sentry initializes the Sentry SDK (github.com/getsentry/sentry-go)
// for dim: Resource initializes it, returns the current Hub and flushes the
// buffered events on shutdown.
//
// # Adding Sentry to a service
//
// A service made by `skit new --full` has no Sentry and does not build
// sentry-go. Copy the parts below to add it; Sentry is on iff SENTRY_DSN is
// set, and the events are tagged with APP_ENV.
//
// internal/app/config/opts.go — the group in ServerOpts and its type:
//
//	Sentry Sentry `group:"sentry" namespace:"sentry" env-namespace:"SENTRY"`
//
//	// Sentry holds the Sentry settings. Sentry is on iff DSN is non-empty, so
//	// an empty SENTRY_DSN turns it off without a separate flag.
//	type Sentry struct {
//	    DSN string `long:"dsn" env:"DSN" description:"Sentry project DSN (empty disables Sentry)" json:"-"`
//	}
//
// internal/app/deps/sentry.go, with initSentry in Initializers right after
// initTelemetry. It initializes at once, so a malformed DSN fails the start:
//
//	var initSentry = func(c *Deps) (dim.CleanupFunc, error) {
//	    if c.Opts.Sentry.DSN == "" {
//	        return nil, nil
//	    }
//	    _, cleanup, err := sentry.Resource(sentry.Config{
//	        DSN:         c.Opts.Sentry.DSN,
//	        Environment: c.Opts.Env,
//	    })(context.Background())
//	    return cleanup, err
//	}
//
// .env.example:
//
//	SENTRY_DSN=
//
// The SDK is global: sentry-go captures through its current Hub, and the
// initializer only sets it up. To send the ERROR records, capture them in the
// Events hook of logger.Config (buildLogger of the service). A file that
// imports both this package and sentry-go names one of them, e.g. sentrygo.
//
// # Config
//
//   - DSN: the project DSN; empty disables the transport.
//   - Environment: tags the events.
//   - Debug: the debug logging of the SDK.
//   - IgnoreErrors: appended to the default ignore list ("context canceled").
//   - FlushTimeout: bounds the flush on cleanup (default 2s).
package sentry
