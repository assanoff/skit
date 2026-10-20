package sentry_test

import (
	"context"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/provider/sentry"
)

func TestResourceWithoutDSN(t *testing.T) {
	is := is.New(t)
	t.Setenv("SENTRY_DSN", "") // the SDK reads it when Config.DSN is empty

	hub, cleanup, err := sentry.Resource(sentry.Config{Environment: "test"})(context.Background())
	is.NoErr(err)
	is.True(hub != nil)
	is.Equal(hub.Client().Options().Environment, "test")
	is.NoErr(cleanup())
}

func TestResourceMalformedDSN(t *testing.T) {
	is := is.New(t)
	hub, cleanup, err := sentry.Resource(sentry.Config{DSN: "not a dsn"})(context.Background())
	is.True(err != nil)
	is.True(hub == nil)
	is.True(cleanup == nil)
}
