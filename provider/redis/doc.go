// Package redis provides a ready Redis client (github.com/redis/go-redis) for
// dim: Resource opens it, checks it with a PING and closes it on shutdown.
//
// # Adding Redis to a service
//
// A service made by `skit new --full` has no Redis and does not build
// go-redis. Copy the parts below to add it; Redis is on iff REDIS_ADDR is set.
//
// internal/app/config/opts.go — the group in ServerOpts and its type:
//
//	Redis Redis `group:"redis" namespace:"redis" env-namespace:"REDIS"`
//
//	// Redis holds the Redis settings. Redis is on iff Addr is non-empty, so
//	// an empty REDIS_ADDR turns it off without a separate flag.
//	type Redis struct {
//	    Addr     string `long:"addr"     env:"ADDR"     description:"redis host:port (empty disables Redis)"`
//	    Password string `long:"password" env:"PASSWORD" description:"redis password (empty for none)" json:"-"`
//	    DB       int    `long:"db"       env:"DB"       description:"redis database index"`
//	}
//
// internal/app/deps/deps.go — the field of Deps, with go-redis imported, and
// initRedis in Initializers after initStore:
//
//	Redis dim.Provider[*redis.Client]
//
// internal/app/deps/redis.go — the client is opened on first use of c.Redis;
// without REDIS_ADDR the provider stays nil:
//
//	var initRedis = func(c *Deps) (cleanup dim.CleanupFunc, err error) {
//	    if c.Opts.Redis.Addr == "" {
//	        return nil, nil
//	    }
//	    c.Redis, cleanup = dim.NewResource("Redis", redis.Resource(redis.Config{
//	        Addr:     c.Opts.Redis.Addr,
//	        Password: c.Opts.Redis.Password,
//	        DB:       c.Opts.Redis.DB,
//	    }))
//	    return cleanup, nil
//	}
//
// .env.example and docker-compose.yml:
//
//	REDIS_ADDR=localhost:6379
//
//	redis:
//	  image: redis:7-alpine
//	  ports: ["6379:6379"]
//
// Resource returns *redis.Client of go-redis; a file that imports both
// packages names one of them, e.g. goredis.
//
// # Config
//
//   - Addr: host:port of the server.
//   - Password: the auth password; empty for none.
//   - DB: the database index.
package redis
