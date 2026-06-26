package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestPacedLoopDrainsBusyImmediately verifies that an adaptive loop re-runs at
// once after a Busy tick (BusyInterval=0) and only waits the idle Interval once
// a tick reports Idle. With a long Interval the busy ticks must all fire well
// before a single interval could elapse.
func TestPacedLoopDrainsBusyImmediately(t *testing.T) {
	const wantBusy = 5
	var calls atomic.Int64
	idle := make(chan struct{}, 1)

	tick := func(context.Context) (Pace, error) {
		n := calls.Add(1)
		if n <= wantBusy {
			return Busy, nil
		}
		select {
		case idle <- struct{}{}:
		default:
		}
		return Idle, nil
	}

	loop := NewPacedLoop(nil, LoopConfig{
		Name:               "test",
		Interval:           10 * time.Second, // idle wait — must NOT be hit during busy drain
		BusyInterval:       0,                // drain immediately
		ImmediateFirstTick: true,
	}, tick)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = loop.Start(ctx) }()

	select {
	case <-idle:
	case <-time.After(2 * time.Second):
		t.Fatalf("busy drain did not reach the idle tick in time; calls=%d", calls.Load())
	}
	cancel()

	if got := calls.Load(); got != wantBusy+1 {
		t.Errorf("calls = %d, want %d (%d busy + 1 idle)", got, wantBusy+1, wantBusy)
	}
}

// TestPacedLoopIdleBackoff checks the geometric idle backoff is capped at
// MaxIdleInterval.
func TestPacedLoopIdleBackoff(t *testing.T) {
	l := &Loop{cfg: LoopConfig{Interval: time.Second, MaxIdleInterval: 5 * time.Second}}
	// 1s -> 2s -> 4s -> capped at 5s -> stays 5s.
	got := []time.Duration{}
	d := l.cfg.Interval
	for range 5 {
		got = append(got, d)
		d = l.growIdle(d)
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("idle delay[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestPacedLoopNoBackoffWhenUncapped keeps the idle delay at Interval when no
// MaxIdleInterval is set.
func TestPacedLoopNoBackoffWhenUncapped(t *testing.T) {
	l := &Loop{cfg: LoopConfig{Interval: time.Second}}
	if d := l.growIdle(time.Second); d != time.Second {
		t.Errorf("uncapped idle delay = %s, want 1s", d)
	}
}
