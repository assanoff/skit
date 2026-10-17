package otel

import (
	"context"
	"testing"

	"github.com/matryer/is"
)

func TestSuppressIsInherited(t *testing.T) {
	is := is.New(t)

	var nilCtx context.Context
	is.True(!IsSuppressed(nilCtx))               // a nil context is not suppressed
	is.True(!IsSuppressed(context.Background())) // a plain context is not suppressed

	ctx := Suppress(context.Background())
	is.True(IsSuppressed(ctx)) // round-trips

	child, cancel := context.WithCancel(ctx)
	defer cancel()
	is.True(IsSuppressed(child)) // every derived context stays suppressed
}

func TestUnsuppressLiftsInheritedMark(t *testing.T) {
	is := is.New(t)

	ctx := Unsuppress(Suppress(t.Context()))
	is.True(!IsSuppressed(ctx)) // lifted

	child, cancel := context.WithCancel(ctx)
	defer cancel()
	is.True(!IsSuppressed(child))          // for the derived contexts too
	is.True(IsSuppressed(Suppress(child))) // and can be set again
}
