package dedup

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/redis/go-redis/v9"
)

// New builds a Keeper on the store the scheme of uri names, so a service
// picks its store in its config:
//
//   - mem:// — the memory of the process, as NewMemory;
//   - redis://[user:password@]host:port/db, rediss:// for TLS, unix:// for a
//     socket — a client of redis.ParseURL, with its query parameters such as
//     dial_timeout, as NewRedis. Close closes the client.
//
// Any other uri, an empty one included, is an error. log is the one of
// NewRedis.
func New(uri string, log *slog.Logger, opts ...Option) (*Keeper, error) {
	scheme, rest, found := strings.Cut(uri, "://")
	if !found {
		return nil, errors.New("dedup: the store uri has no scheme: want mem:// or redis://")
	}
	switch scheme {
	case "mem":
		if rest != "" {
			return nil, fmt.Errorf("dedup: the store mem:// takes nothing after the scheme, got %q", rest)
		}
		return newKeeper(newMemory(), log, opts), nil
	case "redis", "rediss", "unix":
		o, err := redis.ParseURL(uri)
		if err != nil {
			if ue, ok := errors.AsType[*url.Error](err); ok {
				err = ue.Err // the error of url.Parse quotes the uri, password and all
			}
			return nil, fmt.Errorf("dedup: %w", err)
		}
		client := redis.NewClient(o)
		k := newKeeper(redisStore{client: client}, log, opts)
		k.closeStore = client.Close
		return k, nil
	}
	return nil, fmt.Errorf("dedup: unknown store %q: want mem:// or redis://", scheme)
}
