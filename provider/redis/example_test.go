package redis_test

import (
	"context"
	"log/slog"
	"time"

	"github.com/assanoff/skit/closer"
	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/provider/redis"
)

// The client is a resource of the dependencies of a service: opened and
// checked with a PING on first use, closed by the cleanup at shutdown. The
// example needs Redis, so go test builds it but does not run it.
func Example() {
	ctx := context.Background()
	log := slog.Default()
	client, cleanup := dim.NewResource("Redis", redis.Resource(redis.Config{
		Addr: "localhost:6379",
	}))
	closer.Add(cleanup)

	err := client(ctx).Set(ctx, "widget:w-1", "gear", time.Hour).Err()
	if err != nil {
		log.ErrorContext(ctx, "set", "err", err)
	}
}
