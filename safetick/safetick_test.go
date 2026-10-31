package safetick_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/matryer/is"

	"github.com/assanoff/skit/safetick"
)

// runTick invokes fn under RecoverTick's deferred recovery, mirroring how a
// worker tick uses it. It returns normally when the panic is swallowed — if the
// panic escaped, the test goroutine would crash.
func runTick(log *slog.Logger, worker string, onPanic safetick.PanicHandler, fn func()) {
	defer safetick.RecoverTick(context.Background(), log, worker, onPanic)
	fn()
}

func TestRecoverTickSwallowsPanic(t *testing.T) {
	is := is.New(t)

	var (
		gotWorker string
		gotPhase  string
		calls     int
	)
	onPanic := func(worker, phase string) {
		gotWorker = worker
		gotPhase = phase
		calls++
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	runTick(log, "outbox-relay", onPanic, func() {
		panic("boom")
	})

	is.Equal(calls, 1)                  // onPanic invoked exactly once
	is.Equal(gotWorker, "outbox-relay") // worker label forwarded
	is.Equal(gotPhase, "tick")          // RecoverTick labels the phase "tick"
	is.True(bytes.Contains(buf.Bytes(), []byte("worker tick panic recovered")))
}

func TestRecoverTickNoPanicIsNoop(t *testing.T) {
	is := is.New(t)

	calls := 0
	onPanic := func(worker, phase string) {
		calls++
	}

	runTick(nil, "w", onPanic, func() {
		// no panic
	})

	is.Equal(calls, 0) // onPanic not called without a panic
}

func TestRecoverTickNilLoggerAndHandler(t *testing.T) {
	// A nil logger and nil handler must not themselves panic while recovering.
	runTick(nil, "w", nil, func() {
		panic("boom")
	})
}

func TestGuardRecoversAndReturnsTrue(t *testing.T) {
	is := is.New(t)

	var gotPhase string
	onPanic := func(worker, phase string) {
		gotPhase = phase
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	recovered := safetick.Guard(context.Background(), log, "consumer", onPanic, func() {
		panic("kaboom")
	})

	is.True(recovered)          // the panic was recovered
	is.Equal(gotPhase, "guard") // Guard labels the phase "guard"
	is.True(bytes.Contains(buf.Bytes(), []byte("guarded call panic recovered")))
}

func TestGuardNoPanicReturnsFalse(t *testing.T) {
	is := is.New(t)

	ran := false
	recovered := safetick.Guard(context.Background(), nil, "consumer", nil, func() {
		ran = true
	})

	is.True(ran)        // fn ran to completion
	is.True(!recovered) // no panic -> false
}

func TestCallReturnsPanicError(t *testing.T) {
	is := is.New(t)

	err := safetick.Call(func() error {
		panic("boom")
	})

	pe, ok := errors.AsType[*safetick.PanicError](err)
	is.True(ok)                                       // the panic comes back as a *PanicError
	is.Equal(pe.Value, "boom")                        // with the value passed to panic
	is.Equal(err.Error(), "panic: boom")              // and its message
	is.True(bytes.Contains(pe.Stack, []byte("Call"))) // and the stack of the panic
}

func TestCallReturnsError(t *testing.T) {
	is := is.New(t)
	errBoom := errors.New("boom")

	// Call returns the error of fn, and nil when fn returns nil.
	is.Equal(safetick.Call(func() error { return errBoom }), errBoom)
	is.NoErr(safetick.Call(func() error { return nil }))
}
