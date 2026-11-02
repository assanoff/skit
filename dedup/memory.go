package dedup

import (
	"context"
	"maps"
	"sync"
	"time"
)

// sweepEvery is how often acquire drops every expired record of a memory
// store; between sweeps a record expires when it is touched.
const sweepEvery = time.Minute

// NewMemory builds a Keeper whose records live in the memory of the process:
// tests and a service of one replica. Its warnings go to slog.Default.
func NewMemory(opts ...Option) *Keeper {
	return newKeeper(newMemory(), nil, opts)
}

// memory is the store of NewMemory.
type memory struct {
	mu      sync.Mutex
	entries map[string]entry
	swept   time.Time
}

// entry is the record of a key: a running one holds the token of its call, a
// done one holds the stored value and no token.
type entry struct {
	token   string
	value   []byte
	expires time.Time
}

func newMemory() *memory {
	return &memory{entries: map[string]entry{}}
}

func (m *memory) acquire(_ context.Context, key, token string, lease time.Duration) (state, []byte, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweep(now)
	e, ok := m.entries[key]
	if ok && now.Before(e.expires) {
		if e.token != "" {
			return running, nil, nil
		}
		return done, e.value, nil
	}
	m.entries[key] = entry{token: token, expires: now.Add(lease)}
	return acquired, nil, nil
}

func (m *memory) complete(_ context.Context, key, token string, value []byte, ttl time.Duration) (bool, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok || e.token != token || !now.Before(e.expires) {
		return false, nil
	}
	m.entries[key] = entry{value: value, expires: now.Add(ttl)}
	return true, nil
}

func (m *memory) release(_ context.Context, key, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries[key].token == token {
		delete(m.entries, key)
	}
	return nil
}

// sweep drops every expired record, at most once per sweepEvery.
func (m *memory) sweep(now time.Time) {
	if now.Sub(m.swept) < sweepEvery {
		return
	}
	m.swept = now
	maps.DeleteFunc(m.entries, func(_ string, e entry) bool {
		return !now.Before(e.expires)
	})
}
