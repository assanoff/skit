package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/safetick"
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

// deadlineSink records the context its mark call receives, to assert the mark
// gets its own timeout budget rather than the handler's.
type deadlineSink struct {
	markCtx context.Context
}

func (s *deadlineSink) MarkDone(ctx context.Context, _ memItem, _ time.Time) error {
	s.markCtx = ctx
	return nil
}

func (s *deadlineSink) MarkFailed(ctx context.Context, _ memItem, _ string, _ bool, _ time.Time) error {
	s.markCtx = ctx
	return nil
}

func TestProcessorMarkGetsFreshTimeout(t *testing.T) {
	src := SourceFunc[memItem](func(_ context.Context, _ time.Time, _ int) ([]memItem, error) {
		return []memItem{{1}}, nil
	})

	var handleDeadline time.Time
	h := HandlerFunc[memItem](func(ctx context.Context, _ memItem) error {
		handleDeadline, _ = ctx.Deadline()
		return nil
	})

	sink := &deadlineSink{}
	p := NewProcessor[memItem](nil, src, h, sink, ProcessorConfig{HandleTimeout: 200 * time.Millisecond})

	if err := p.Tick()(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	markDeadline, ok := sink.markCtx.Deadline()
	if !ok {
		t.Fatal("mark ctx has no deadline")
	}
	// A fresh budget means the mark's deadline is strictly later than the
	// handler's; if they shared one context the deadlines would be equal.
	if !markDeadline.After(handleDeadline) {
		t.Fatalf("mark deadline %v should be after handle deadline %v (independent budget)", markDeadline, handleDeadline)
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

// releasingStore is a memStore that is also a Releaser.
type releasingStore struct {
	*memStore

	released []memItem
}

func (s *releasingStore) Release(_ context.Context, items []memItem) error {
	s.released = append(s.released, items...)
	return nil
}

// An error IsHalt accepts stops the batch and the rest goes back; any other
// error concerns its item only.
func TestProcessorIsHalt(t *testing.T) {
	is := is.New(t)
	errTerminal := errors.New("bad item")
	errItem := errors.New("item failed")
	errDown := errors.New("broker down")
	store := &releasingStore{memStore: newMemStore(memItem{1}, memItem{2}, memItem{3}, memItem{4})}
	var handled []int
	h := HandlerFunc[memItem](func(_ context.Context, it memItem) error {
		handled = append(handled, it.id)
		return map[int]error{1: errTerminal, 2: errItem, 3: errDown}[it.id]
	})
	p := NewProcessor[memItem](nil, store, h, store, ProcessorConfig{
		BatchSize:  4,
		IsTerminal: func(err error) bool { return errors.Is(err, errTerminal) },
		IsHalt:     func(err error) bool { return errors.Is(err, errDown) },
	})

	pace, err := p.PacedTick()(t.Context())
	is.NoErr(err)
	is.Equal(handled, []int{1, 2, 3})                // other failures go on, a halting one stops
	is.Equal(len(store.failed), 3)                   // every failure is marked
	is.True(store.terminal[1] && !store.terminal[3]) // with its own terminality
	is.Equal(store.released, []memItem{{4}})         // the rest goes back untouched
	is.Equal(pace, Idle)                             // a full but stopped batch does not drain again at once
}

func TestProcessorIsHaltWithoutReleaser(t *testing.T) {
	is := is.New(t)
	store := newMemStore(memItem{1}, memItem{2})
	var handled int
	h := HandlerFunc[memItem](func(context.Context, memItem) error {
		handled++
		return errors.New("down")
	})
	p := NewProcessor[memItem](nil, store, h, store, ProcessorConfig{
		IsHalt: func(error) bool { return true },
	})

	is.NoErr(p.Tick()(t.Context()))
	is.Equal(handled, 1) // stopped; the rest stays claimed for the source to reclaim
}

func TestProcessorFullBatchIsBusy(t *testing.T) {
	is := is.New(t)
	store := newMemStore(memItem{1}, memItem{2})
	h := HandlerFunc[memItem](func(context.Context, memItem) error {
		return nil
	})
	p := NewProcessor[memItem](nil, store, h, store, ProcessorConfig{BatchSize: 2})

	pace, err := p.PacedTick()(t.Context())
	is.NoErr(err)
	is.Equal(pace, Busy)
}

// A panic in Handle is recovered per item: by default the item is retried with
// the panic as its error, and the batch goes on.
func TestProcessorPanicRetries(t *testing.T) {
	is := is.New(t)
	store := newMemStore(memItem{1}, memItem{2}, memItem{3})
	h := HandlerFunc[memItem](func(_ context.Context, it memItem) error {
		if it.id == 2 {
			panic("boom")
		}
		return nil
	})
	p := NewProcessor[memItem](nil, store, h, store, ProcessorConfig{})

	is.NoErr(p.Tick()(t.Context()))
	is.Equal(store.done, []memItem{{1}, {3}}) // the batch goes on past the panic
	is.Equal(store.failed, []memItem{{2}})    // the panicked item is marked failed
	is.Equal(store.terminal[2], false)        // as retryable
	is.Equal(store.failMsg[2], "panic: boom") // with the panic as its error
}

// OnPanic decides the outcome of a panicked item: nil marks it done, an error
// IsTerminal accepts marks it terminal.
func TestProcessorOnPanic(t *testing.T) {
	is := is.New(t)
	errDead := errors.New("dead")
	store := newMemStore(memItem{1}, memItem{2})
	h := HandlerFunc[memItem](func(_ context.Context, it memItem) error {
		panic(it.id)
	})
	var seen []any
	p := NewProcessor[memItem](nil, store, h, store, ProcessorConfig{
		IsTerminal: func(err error) bool { return errors.Is(err, errDead) },
		OnPanic: func(_ context.Context, item any, pe *safetick.PanicError) error {
			seen = append(seen, item, pe.Value)
			if item.(memItem).id == 1 {
				return nil
			}
			return errDead
		},
	})

	is.NoErr(p.Tick()(t.Context()))
	is.Equal(seen, []any{memItem{1}, 1, memItem{2}, 2}) // OnPanic gets the item and the panic value
	is.Equal(store.done, []memItem{{1}})                // nil marks the item done
	is.Equal(store.failed, []memItem{{2}})              // an error marks it failed
	is.Equal(store.terminal[2], true)                   // terminal as IsTerminal says
}

// settleStore is a releasingStore that counts the marks and releases it got
// with a done context.
type settleStore struct {
	*releasingStore

	doneCtx int
}

func (s *settleStore) MarkDone(ctx context.Context, item memItem, now time.Time) error {
	if ctx.Err() != nil {
		s.doneCtx++
	}
	return s.memStore.MarkDone(ctx, item, now)
}

func (s *settleStore) Release(ctx context.Context, items []memItem) error {
	if ctx.Err() != nil {
		s.doneCtx++
	}
	return s.releasingStore.Release(ctx, items)
}

// A tick canceled mid-batch still marks the item it handled and releases the
// rest instead of handling it.
func TestProcessorCanceledMidBatch(t *testing.T) {
	is := is.New(t)
	ctx, cancel := context.WithCancel(t.Context())
	store := &settleStore{releasingStore: &releasingStore{
		memStore: newMemStore(memItem{1}, memItem{2}, memItem{3}),
	}}
	var handled []int
	h := HandlerFunc[memItem](func(_ context.Context, it memItem) error {
		handled = append(handled, it.id)
		cancel() // shutdown while the item is handled
		return nil
	})
	p := NewProcessor[memItem](nil, store, h, store, ProcessorConfig{})

	is.NoErr(p.Tick()(ctx))
	is.Equal(handled, []int{1})                   // no item starts after the cancel
	is.Equal(store.done, []memItem{{1}})          // the handled item is marked
	is.Equal(store.released, []memItem{{2}, {3}}) // the rest goes back
	is.Equal(store.doneCtx, 0)                    // neither on the canceled context
}

// BatchTimeout stops the batch before its next item once it has run that long
// since the claim, and releases the rest.
func TestProcessorBatchTimeout(t *testing.T) {
	is := is.New(t)
	store := &releasingStore{memStore: newMemStore(memItem{1}, memItem{2}, memItem{3}, memItem{4})}
	clock := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	var handled []int
	h := HandlerFunc[memItem](func(_ context.Context, it memItem) error {
		handled = append(handled, it.id)
		clock = clock.Add(time.Second) // each item takes a second
		return nil
	})
	p := NewProcessor[memItem](nil, store, h, store, ProcessorConfig{
		BatchSize:    4,
		BatchTimeout: 2 * time.Second,
		Now:          func() time.Time { return clock },
	})

	pace, err := p.PacedTick()(t.Context())
	is.NoErr(err)
	is.Equal(handled, []int{1, 2})                // the third item would start at the timeout
	is.Equal(store.released, []memItem{{3}, {4}}) // so the rest goes back
	is.Equal(pace, Busy)                          // a full batch still drains again at once
}
