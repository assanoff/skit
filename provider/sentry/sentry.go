package sentry

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/assanoff/skit/dim"
)

// defaultFlushTimeout bounds the flush on cleanup when Config.FlushTimeout is
// zero.
const defaultFlushTimeout = 2 * time.Second

// Config configures the Sentry SDK.
type Config struct {
	// DSN is the Sentry project DSN. An empty DSN disables the transport: the
	// SDK initializes and drops the events.
	DSN string
	// Environment tags the events, e.g. "production".
	Environment string
	// Debug enables the debug logging of the Sentry SDK.
	Debug bool
	// IgnoreErrors is appended to the default ignore list ("context canceled").
	IgnoreErrors []string
	// FlushTimeout bounds the flush on cleanup (default 2s).
	FlushTimeout time.Duration
}

// Resource returns a dim factory that initializes the Sentry SDK from cfg and
// returns the current Hub. The cleanup flushes the buffered events, bounded by
// FlushTimeout.
func Resource(cfg Config) func(ctx context.Context) (*sentry.Hub, dim.CleanupFunc, error) {
	return func(context.Context) (*sentry.Hub, dim.CleanupFunc, error) {
		err := sentry.Init(sentry.ClientOptions{
			Dsn:          cfg.DSN,
			Environment:  cfg.Environment,
			Debug:        cfg.Debug,
			IgnoreErrors: append([]string{"context canceled"}, cfg.IgnoreErrors...),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("sentry: init: %w", err)
		}
		flush := cmp.Or(cfg.FlushTimeout, defaultFlushTimeout)
		cleanup := func() error {
			sentry.Flush(flush)
			return nil
		}
		return sentry.CurrentHub(), cleanup, nil
	}
}
