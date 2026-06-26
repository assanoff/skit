package provider

import (
	"context"
	"fmt"
	"time"

	sentry "github.com/getsentry/sentry-go"

	"github.com/assanoff/skit/dim"
)

// SentryConfig configures Sentry initialization.
type SentryConfig struct {
	// DSN is the Sentry project DSN. An empty DSN disables transport (the SDK
	// still initializes; events are dropped) — gate on it in your initializer if
	// you prefer not to init at all.
	DSN string
	// Environment tags events (e.g. "production").
	Environment string
	// Debug enables the Sentry SDK's debug logging.
	Debug bool
	// IgnoreErrors is appended to the default ignore list ("context canceled").
	IgnoreErrors []string
	// FlushTimeout bounds the flush performed on cleanup (default 2s).
	FlushTimeout time.Duration
}

var defaultSentryIgnoreErrors = []string{"context canceled"}

// Sentry returns a dim factory that initializes the Sentry SDK from cfg and
// returns the current Hub. The cleanup flushes buffered events, bounded by
// FlushTimeout.
func Sentry(cfg SentryConfig) func(ctx context.Context) (*sentry.Hub, dim.CleanupFunc, error) {
	return func(ctx context.Context) (*sentry.Hub, dim.CleanupFunc, error) {
		ignore := append(append([]string{}, defaultSentryIgnoreErrors...), cfg.IgnoreErrors...)
		if err := sentry.Init(sentry.ClientOptions{
			Dsn:          cfg.DSN,
			Environment:  cfg.Environment,
			Debug:        cfg.Debug,
			IgnoreErrors: ignore,
		}); err != nil {
			return nil, nil, fmt.Errorf("provider: init sentry: %w", err)
		}

		flush := cfg.FlushTimeout
		if flush == 0 {
			flush = 2 * time.Second
		}
		cleanup := func() error {
			sentry.Flush(flush)
			return nil
		}
		return sentry.CurrentHub(), cleanup, nil
	}
}
