package provider

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/assanoff/skit/dim"
)

// RedisConfig configures the Redis client.
type RedisConfig struct {
	// Addr is the host:port, e.g. "localhost:6379".
	Addr string
	// Password is the auth password (empty for none).
	Password string
	// DB is the database index.
	DB int
}

// Redis returns a dim factory that opens a go-redis client from cfg and verifies
// it with a PING. The cleanup closes the client.
func Redis(cfg RedisConfig) func(ctx context.Context) (*redis.Client, dim.CleanupFunc, error) {
	return func(ctx context.Context) (*redis.Client, dim.CleanupFunc, error) {
		client := redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
		if err := client.Ping(ctx).Err(); err != nil {
			_ = client.Close()
			return nil, nil, fmt.Errorf("provider: ping redis: %w", err)
		}
		return client, func() error { return client.Close() }, nil
	}
}
