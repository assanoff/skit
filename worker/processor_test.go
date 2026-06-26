package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

// memItem is a trivial work item keyed by id.
type memItem struct{ id int }

// memStore is an in-memory Source+Sink recording outcomes for assertions.
type memStore struct {
	pending []memItem
	claimed bool

	done     []memItem
	failed   []memItem
	terminal map[int]bool
	failMsg  map[int]string
}

func newMemStore(items ...memItem) *memStore {
	return &memStore{
		pending:  items,
		terminal: map[int]bool{},
		failMsg:  map[int]string{},
	}
}

func (m *memStore) Claim(_ context.Context, _ time.Time, limit int) ([]memItem, error) {
	if m.claimed {
		return nil, nil // second tick: nothing left
	}
	m.claimed = true
	if limit < len(m.pending) {
		return m.pending[:limit], nil
	}
	return m.pending, nil
}

func (m *memStore) MarkDone(_ context.Context, item memItem, _ time.Time) error {
	m.done = append(m.done, item)
	return nil
}

func (m *memStore) MarkFailed(_ context.Context, item memItem, errMsg string, terminal bool, _ time.Time) error {
	m.failed = append(m.failed, item)
	m.terminal[item.id] = terminal
	m.failMsg[item.id] = errMsg
	return nil
}

func TestProcessorHappyPath(t *testing.T) {
	store := newMemStore(memItem{1}, memItem{2}, memItem{3})
	var handled []int
	p := NewProcessor[memItem](nil, store, HandlerFunc[memItem](func(_ context.Context, it memItem) error {
		handled = append(handled, it.id)
		return nil
	}), store, ProcessorConfig{Name: "test"})

	if err := p.Tick()(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(handled) != 3 || len(store.done) != 3 {
		t.Fatalf("expected 3 handled+done, got handled=%d done=%d", len(handled), len(store.done))
	}
	if len(store.failed) != 0 {
		t.Errorf("expected no failures, got %d", len(store.failed))
	}
}

func TestProcessorRetryableVsTerminal(t *testing.T) {
	errTerminal := errors.New("bad request: do not retry")
	errTransient := errors.New("temporary network blip")

	store := newMemStore(memItem{1}, memItem{2})
	p := NewProcessor[memItem](nil, store, HandlerFunc[memItem](func(_ context.Context, it memItem) error {
		if it.id == 1 {
			return errTerminal
		}
		return errTransient
	}), store, ProcessorConfig{
		IsTerminal: func(err error) bool { return errors.Is(err, errTerminal) },
	})

	if err := p.Tick()(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(store.failed) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(store.failed))
	}
	if !store.terminal[1] {
		t.Error("item 1 should be marked terminal")
	}
	if store.terminal[2] {
		t.Error("item 2 should be retryable, not terminal")
	}
	if len(store.done) != 0 {
		t.Errorf("expected no successes, got %d", len(store.done))
	}
}

func TestProcessorSanitizesErrorMessage(t *testing.T) {
	store := newMemStore(memItem{1})
	p := NewProcessor[memItem](nil, store, HandlerFunc[memItem](func(context.Context, memItem) error {
		return errors.New("auth failed token=supersecret")
	}), store, ProcessorConfig{})

	if err := p.Tick()(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := store.failMsg[1]; got != "auth failed token=***" {
		t.Errorf("expected sanitized message, got %q", got)
	}
}

func TestProcessorEmptyClaimIsNoOp(t *testing.T) {
	store := newMemStore() // nothing pending
	called := false
	p := NewProcessor[memItem](nil, store, HandlerFunc[memItem](func(context.Context, memItem) error {
		called = true
		return nil
	}), store, ProcessorConfig{})

	if err := p.Tick()(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if called {
		t.Error("handler should not be called on an empty claim")
	}
}

func TestProcessorSurfacesClaimError(t *testing.T) {
	wantErr := errors.New("db down")
	src := SourceFunc[memItem](func(context.Context, time.Time, int) ([]memItem, error) {
		return nil, wantErr
	})
	sink := newMemStore()
	p := NewProcessor[memItem](nil, src, HandlerFunc[memItem](func(context.Context, memItem) error {
		return nil
	}), sink, ProcessorConfig{})

	if err := p.Tick()(context.Background()); !errors.Is(err, wantErr) {
		t.Errorf("expected claim error surfaced, got %v", err)
	}
}
