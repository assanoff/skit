package safetick_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/assanoff/skit/safetick"
)

// A loop defers RecoverTick at the top of each tick, so a panic ends the tick,
// not the loop.
func ExampleRecoverTick() {
	log := slog.New(slog.DiscardHandler)
	onPanic := func(worker, phase string) {
		fmt.Println("recovered:", worker, phase)
	}
	tick := func(ctx context.Context, n int) {
		defer safetick.RecoverTick(ctx, log, "report", onPanic)
		if n == 2 {
			panic("nil map")
		}
		fmt.Println("tick", n)
	}

	for n := 1; n <= 3; n++ {
		tick(context.Background(), n)
	}
	// Output:
	// tick 1
	// recovered: report tick
	// tick 3
}

// Guard reports a recovered panic inline, so the caller decides what follows.
func ExampleGuard() {
	log := slog.New(slog.DiscardHandler)
	panicked := safetick.Guard(context.Background(), log, "consumer", nil, func() {
		panic("bad message")
	})
	fmt.Println("panicked:", panicked)
	// Output:
	// panicked: true
}

// Call returns a panic as a *PanicError, with no logging, so the caller
// decides what the panic means.
func ExampleCall() {
	err := safetick.Call(func() error {
		panic("bad item")
	})
	if pe, ok := errors.AsType[*safetick.PanicError](err); ok {
		fmt.Println(pe)
	}
	// Output:
	// panic: bad item
}
