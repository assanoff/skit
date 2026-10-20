package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/assanoff/skit/dim"
)

// Config configures the Redis client.
type Config struct {
	// Addr is the host:port, e.g. "localhost:6379".
	Addr string
	// Password is the auth password (empty for none).
	Password string
	// DB is the database index.
	DB int
}

// Resource returns a dim factory that opens a go-redis client from cfg and
// verifies it with a PING. The cleanup closes the client.
func Resource(cfg Config) func(ctx context.Context) (*redis.Client, dim.CleanupFunc, error) {
	return func(ctx context.Context) (*redis.Client, dim.CleanupFunc, error) {
		client := redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
		if err := client.Ping(ctx).Err(); err != nil {
			_ = client.Close()
			return nil, nil, fmt.Errorf("redis: ping: %w", err)
		}
		return client, client.Close, nil
	}
}
