package sentry_test

import (
	"context"
	"fmt"

	"github.com/assanoff/skit/provider/sentry"
)

// The initializer of a service calls the factory at once, so a malformed DSN
// fails the start. An empty DSN keeps the SDK on but sends nothing, as in
// development; the cleanup flushes the buffered events.
func Example() {
	hub, cleanup, err := sentry.Resource(sentry.Config{
		Environment: "staging",
		Release:     "orders@1.4.0",
	})(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = cleanup() }()

	opts := hub.Client().Options()
	fmt.Println(opts.Environment, opts.Release, opts.Dsn == "")

	_, _, err = sentry.Resource(sentry.Config{DSN: "not a dsn"})(context.Background())
	fmt.Println(err != nil)
	// Output:
	// staging orders@1.4.0 true
	// true
}
