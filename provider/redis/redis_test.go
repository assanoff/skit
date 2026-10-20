package redis_test

import (
	"context"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/provider/redis"
)

func TestResourceUnreachable(t *testing.T) {
	is := is.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	client, cleanup, err := redis.Resource(redis.Config{Addr: "127.0.0.1:1"})(ctx)
	is.True(err != nil) // nothing listens on port 1
	is.True(client == nil)
	is.True(cleanup == nil) // the client is closed already
}
