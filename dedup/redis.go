package dedup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Records of a Redis store: the key of the caller under redisPrefix, its value
// runningMark and the token while fn runs, doneMark and the stored success
// after it. The key expires by itself: at the lease, then at the TTL.
const (
	redisPrefix = "dedup:"
	runningMark = "p:"
	doneMark    = "d:"
)

// NewRedis builds a Keeper whose records live in Redis under the prefix
// "dedup:", over client (a *redis.Client or *redis.ClusterClient). log
// receives the failures to complete or release a key; nil means slog.Default.
func NewRedis(client redis.Cmdable, log *slog.Logger, opts ...Option) *Keeper {
	return newKeeper(redisStore{client: client}, log, opts)
}

// redisStore is the store of NewRedis. Each operation is one script on one
// key, so it is atomic and fits Redis Cluster.
type redisStore struct {
	client redis.Cmdable
}

// acquireScript returns the record of the key, or sets the running record of
// the token for the lease and returns nil.
var acquireScript = redis.NewScript(`
local v = redis.call("get", KEYS[1])
if v then
	return v
end
redis.call("set", KEYS[1], ARGV[1], "px", ARGV[2])
return false`)

// completeScript replaces the running record of the token with the done
// record for the TTL. Returns 1 if it did, 0 if the token no longer holds the
// key.
var completeScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	redis.call("set", KEYS[1], ARGV[2], "px", ARGV[3])
	return 1
end
return 0`)

// releaseScript deletes the key iff it holds the running record of the token.
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0`)

func (s redisStore) acquire(ctx context.Context, key, token string, lease time.Duration) (state, []byte, error) {
	keys := []string{redisPrefix + key}
	v, err := acquireScript.Run(ctx, s.client, keys, runningMark+token, millis(lease)).Text()
	if errors.Is(err, redis.Nil) {
		return acquired, nil, nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("redis acquire: %w", err)
	}
	if strings.HasPrefix(v, runningMark) {
		return running, nil, nil
	}
	value, ok := strings.CutPrefix(v, doneMark)
	if !ok {
		return 0, nil, fmt.Errorf("redis acquire: key %q is not a dedup record", key)
	}
	return done, []byte(value), nil
}

func (s redisStore) complete(ctx context.Context, key, token string, value []byte, ttl time.Duration) (bool, error) {
	keys := []string{redisPrefix + key}
	args := []any{runningMark + token, doneMark + string(value), millis(ttl)}
	n, err := completeScript.Run(ctx, s.client, keys, args...).Int()
	if err != nil {
		return false, fmt.Errorf("redis complete: %w", err)
	}
	return n == 1, nil
}

func (s redisStore) release(ctx context.Context, key, token string) error {
	keys := []string{redisPrefix + key}
	if err := releaseScript.Run(ctx, s.client, keys, runningMark+token).Err(); err != nil {
		return fmt.Errorf("redis release: %w", err)
	}
	return nil
}

// millis is d in whole milliseconds for PX, at least 1.
func millis(d time.Duration) int64 {
	return max(d.Milliseconds(), 1)
}
