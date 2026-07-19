package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a Locker backed by Redis. It acquires with SET key token NX PX ttl
// (atomic "set if absent with expiry") and releases with a Lua script that
// deletes the key only if it still holds this holder's token. The token fence
// stops one holder from freeing a lock that already expired and was retaken by
// another — the classic Redlock single-instance release.
type Redis struct {
	client redis.Cmdable
	log    *slog.Logger
}

// NewRedis builds a Redis Locker over client (a *redis.Client or *redis.ClusterClient).
// log may be nil.
func NewRedis(client redis.Cmdable, log *slog.Logger) *Redis {
	if log == nil {
		log = slog.Default()
	}
	return &Redis{client: client, log: log}
}

var _ Locker = (*Redis)(nil)

// releaseScript deletes the key iff its value still equals the holder's token.
// Returns 1 if it deleted, 0 if the token no longer matched (already expired or
// retaken).
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
else
	return 0
end`)

// TryLock runs SET key token NX PX ttl. ok==false means the key already exists
// (another holder). ttl must be > 0; it bounds how long a crashed holder blocks
// others.
func (r *Redis) TryLock(ctx context.Context, key string, ttl time.Duration) (Release, bool, error) {
	if ttl <= 0 {
		return noopRelease, false, fmt.Errorf("lock: redis requires ttl > 0, got %s", ttl)
	}

	token, err := newToken()
	if err != nil {
		return noopRelease, false, err
	}

	ok, err := r.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return noopRelease, false, fmt.Errorf("lock: redis SET NX: %w", err)
	}
	if !ok {
		return noopRelease, false, nil // held elsewhere
	}

	release := func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := releaseScript.Run(rctx, r.client, []string{key}, token).Err(); err != nil && err != redis.Nil {
			r.log.Warn("lock: redis release failed", "key", key, "error", err)
		}
	}
	return release, true, nil
}

// newToken returns a random 128-bit hex token uniquely identifying a lock holder.
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("lock: generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
