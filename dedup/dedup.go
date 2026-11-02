package dedup

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultTTL   = 24 * time.Hour
	defaultLease = time.Minute
	// settleTimeout bounds the completion and the release of a key, which run
	// past the end of ctx.
	settleTimeout = 5 * time.Second
)

var (
	// ErrInProgress means another call holds the key: its fn is still running.
	ErrInProgress = errors.New("dedup: in progress")
	// ErrUnavailable means the store failed and fn was not called.
	ErrUnavailable = errors.New("dedup: store unavailable")

	errEmptyKey  = errors.New("dedup: empty key")
	errLeaseLost = errors.New("the lease expired before completion")
)

// Keeper runs an operation once per key and hands the stored result of its
// first success to the repeats. It is safe for concurrent use.
type Keeper struct {
	st    store
	ttl   time.Duration
	lease time.Duration
	log   *slog.Logger
	// closeStore releases what New opened for the store; nil when it opened
	// nothing.
	closeStore func() error
}

// Option customizes a Keeper.
type Option func(*options)

type options struct {
	ttl   time.Duration
	lease time.Duration
}

// WithTTL sets how long the stored success of a key is handed to repeats,
// counted from its completion. The default is 24h.
func WithTTL(d time.Duration) Option {
	return func(o *options) { o.ttl = d }
}

// WithLease sets the deadline of fn and the life of the record of a running
// call: a crashed holder blocks its key no longer than that. The default is
// 1m.
func WithLease(d time.Duration) Option {
	return func(o *options) { o.lease = d }
}

func newKeeper(st store, log *slog.Logger, opts []Option) *Keeper {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return &Keeper{
		st:    st,
		ttl:   cmp.Or(max(o.ttl, 0), defaultTTL),
		lease: cmp.Or(max(o.lease, 0), defaultLease),
		log:   cmp.Or(log, slog.Default()),
	}
}

// Close closes the Redis client New opened. It does nothing for a Keeper of
// NewMemory, or of NewRedis, whose client belongs to the caller.
func (k *Keeper) Close() error {
	if k.closeStore == nil {
		return nil
	}
	return k.closeStore()
}

// Do returns the stored result of key, or runs fn and stores its success for
// the TTL. A repeat while fn runs gets ErrInProgress. An error of fn is
// returned as it is, with the value of fn, and frees the key, as a panic of fn
// does; the key is never stored then. A failure of the store before fn wraps
// ErrUnavailable.
func (k *Keeper) Do[T any](ctx context.Context, key string, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	if key == "" {
		return zero, errEmptyKey
	}
	token := rand.Text()
	st, stored, err := k.st.acquire(ctx, key, token, k.lease)
	if err != nil {
		return zero, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if st == running {
		return zero, ErrInProgress
	}
	if st == done {
		return decode[T](key, stored)
	}

	kept := false
	defer func() {
		if !kept {
			k.release(ctx, key, token)
		}
	}()
	fctx, cancel := context.WithTimeout(ctx, k.lease)
	defer cancel()
	v, err := fn(fctx)
	if err != nil {
		return v, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return zero, fmt.Errorf("dedup: encode %q: %w", key, err)
	}
	kept = true
	k.complete(ctx, key, token, b)
	return v, nil
}

// decode returns the stored success of key as a T.
func decode[T any](key string, stored []byte) (T, error) {
	var v T
	if err := json.Unmarshal(stored, &v); err != nil {
		var zero T
		return zero, fmt.Errorf("dedup: decode %q: %w", key, err)
	}
	return v, nil
}

// complete stores the success of key. A failure is logged, not returned: the
// caller has the result of fn and cannot repair the store, and a repeat may
// run fn again.
func (k *Keeper) complete(ctx context.Context, key, token string, value []byte) {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	ok, err := k.st.complete(sctx, key, token, value, k.ttl)
	if err == nil && !ok {
		err = errLeaseLost
	}
	if err != nil {
		k.log.WarnContext(ctx, "dedup: result not recorded", "key", key, "err", err)
	}
}

// release frees key after a failed fn. A failure is logged: the key frees
// itself when its lease ends.
func (k *Keeper) release(ctx context.Context, key, token string) {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	if err := k.st.release(sctx, key, token); err != nil {
		k.log.WarnContext(ctx, "dedup: release failed", "key", key, "err", err)
	}
}

// state is what acquire found under a key.
type state int

const (
	acquired state = iota // the key was free and is now held by the token
	running               // another call holds the key
	done                  // the key holds a stored success
)

// store keeps the records of the keys. A running record holds the token of
// its call and lives for the lease; a done record holds the stored success and
// lives for the TTL.
type store interface {
	// acquire holds key with token for lease, or reports the state of key
	// and, when done, its stored value.
	acquire(ctx context.Context, key, token string, lease time.Duration) (state, []byte, error)
	// complete stores value under key for ttl when token holds key; false
	// means it does not.
	complete(ctx context.Context, key, token string, value []byte, ttl time.Duration) (bool, error)
	// release deletes key when token holds it.
	release(ctx context.Context, key, token string) error
}
