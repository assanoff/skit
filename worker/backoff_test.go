package worker

import (
	"testing"
	"time"
)

func TestBackoffNext(t *testing.T) {
	b := Backoff{Base: 100 * time.Millisecond, Max: time.Second, Factor: 2}

	want := []time.Duration{
		100 * time.Millisecond, // attempt 1
		200 * time.Millisecond, // attempt 2
		400 * time.Millisecond, // attempt 3
		800 * time.Millisecond, // attempt 4
		time.Second,            // attempt 5 capped
		time.Second,            // attempt 6 capped
	}
	for i, w := range want {
		if got := b.Next(i + 1); got != w {
			t.Errorf("attempt %d: got %s want %s", i+1, got, w)
		}
	}
}

func TestBackoffExhausted(t *testing.T) {
	b := Backoff{MaxAttempts: 3}
	if b.Exhausted(2) {
		t.Error("2 attempts should not be exhausted")
	}
	if !b.Exhausted(3) {
		t.Error("3 attempts should be exhausted")
	}

	if (Backoff{MaxAttempts: 0}).Exhausted(1000) {
		t.Error("zero budget means never exhausted")
	}
}

func TestBackoffJitterBounds(t *testing.T) {
	b := Backoff{Base: time.Second, Factor: 2, Jitter: 0.2}
	// randFraction at extremes maps to ±20%.
	if got := b.NextWithRand(1, 0); got != 800*time.Millisecond {
		t.Errorf("min jitter: got %s want 800ms", got)
	}
	if got := b.NextWithRand(1, 1); got != 1200*time.Millisecond {
		t.Errorf("max jitter: got %s want 1200ms", got)
	}
}
