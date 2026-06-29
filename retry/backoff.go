package retry

import (
	"math"
	"time"
)

// Backoff computes retry delays with exponential growth and optional jitter,
// capped at Max, and exposes a max-attempts budget. It is shared by retrying
// HTTP clients, reliable processors, and the Do helper so backoff policy lives
// in one place.
type Backoff struct {
	// Base is the delay for the first retry (attempt 1).
	Base time.Duration
	// Max caps the delay (0 = uncapped).
	Max time.Duration
	// Factor is the multiplier per attempt (defaults to 2 when <= 1).
	Factor float64
	// MaxAttempts is the retry budget; Exhausted reports when it is spent.
	MaxAttempts int
	// Jitter in [0,1] randomizes the delay by ±(jitter*delay). The caller
	// supplies the random fraction to NextWithRand to keep Backoff deterministic
	// and testable.
	Jitter float64
}

// Next returns the delay before the given attempt (1-based), without jitter.
func (b Backoff) Next(attempt int) time.Duration {
	return b.NextWithRand(attempt, 0)
}

// NextWithRand is Next with an explicit random fraction in [0,1) used to apply
// jitter. Pass rand.Float64() in production; pass a fixed value in tests.
func (b Backoff) NextWithRand(attempt int, randFraction float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	factor := b.Factor
	if factor <= 1 {
		factor = 2
	}

	delay := float64(b.Base) * math.Pow(factor, float64(attempt-1))
	if b.Max > 0 && delay > float64(b.Max) {
		delay = float64(b.Max)
	}

	if b.Jitter > 0 {
		// Map randFraction in [0,1) to [-1,1) and scale by jitter.
		offset := (randFraction*2 - 1) * b.Jitter
		delay += delay * offset
	}
	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

// Exhausted reports whether attempts has reached the MaxAttempts budget.
// A non-positive MaxAttempts means "never exhausted".
func (b Backoff) Exhausted(attempts int) bool {
	return b.MaxAttempts > 0 && attempts >= b.MaxAttempts
}
