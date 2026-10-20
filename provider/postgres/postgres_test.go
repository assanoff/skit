package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/provider/postgres"
)

func TestResourceUnreachable(t *testing.T) {
	is := is.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	cfg := dbx.Config{Host: "127.0.0.1:1", Name: "x", DisableTLS: true}
	db, cleanup, err := postgres.Resource(cfg)(ctx)
	is.True(err != nil) // nothing listens on port 1
	is.True(db == nil)
	is.True(cleanup == nil) // the pool is closed already
}
