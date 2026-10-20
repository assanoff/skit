package poller

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/otel"
)

// The tests that wait on time run in a synctest bubble: the ticker and the
// poll timeout advance a fake clock, so they are instant and deterministic.

func TestNewDefaults(t *testing.T) {
	is := is.New(t)

	p := New(nil, 0, func(context.Context) (int, error) {
		return 0, nil
	}, Config{Interval: -time.Second})

	is.Equal(p.Name(), "poller")          // Name defaults to "poller"
	is.Equal(p.cfg.Interval, time.Second) // a non-positive Interval → 1s
}

func TestPollerStartSeedsAndRefreshes(t *testing.T) {
	synctest.Test(t, testPollerStartSeedsAndRefreshes)
}

func testPollerStartSeedsAndRefreshes(t *testing.T) {
	is := is.New(t)

	n := 0
	p := New(nil, -1, func(context.Context) (int, error) {
		n++
		return n, nil
	}, Config{Name: "test", Interval: 5 * time.Millisecond})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()

	synctest.Wait()
	is.Equal(p.Current(), 1) // Start polls at once, before the first Interval

	synctest.Sleep(10 * time.Millisecond)
	is.Equal(p.Current(), 3) // then once per Interval

	cancel()
	is.True(errors.Is(<-done, context.Canceled)) // Start returns on cancel
}

func TestPollerStopsDuringPoll(t *testing.T) {
	synctest.Test(t, testPollerStopsDuringPoll)
}

func testPollerStopsDuringPoll(t *testing.T) {
	is := is.New(t)

	var pollErr error
	p := New(nil, "initial", func(ctx context.Context) (string, error) {
		<-ctx.Done() // a slow source that honors its context
		return "", ctx.Err()
	}, Config{Interval: time.Hour}, WithOnError(func(err error) {
		pollErr = err
	}))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()

	synctest.Wait() // the first poll is in flight, blocked in the Getter
	cancel()
	is.True(errors.Is(<-done, context.Canceled))  // Start returns on cancel
	is.True(errors.Is(pollErr, context.Canceled)) // the Getter sees it
	is.Equal(p.Current(), "initial")              // the value stays
	is.NoErr(p.Stop(t.Context()))
}

func TestPollerPollTimeout(t *testing.T) {
	synctest.Test(t, testPollerPollTimeout)
}

func testPollerPollTimeout(t *testing.T) {
	is := is.New(t)

	var pollErr error
	p := New(nil, "initial", func(ctx context.Context) (string, error) {
		<-ctx.Done() // a source that hangs until the timeout
		return "", ctx.Err()
	}, Config{
		Interval:    time.Hour,
		PollTimeout: 5 * time.Second,
	}, WithOnError(func(err error) { pollErr = err }))

	start := time.Now()
	p.Poll(t.Context())

	is.Equal(time.Since(start), 5*time.Second)            // PollTimeout bounds the poll
	is.True(errors.Is(pollErr, context.DeadlineExceeded)) // and fails it
	is.Equal(p.Current(), "initial")                      // the value stays
}

func TestPollerPollCanceledContext(t *testing.T) {
	is := is.New(t)

	var pollErr error
	p := New(nil, "initial", func(ctx context.Context) (string, error) {
		return "fresh", ctx.Err()
	}, Config{
		Interval:    time.Hour,
		PollTimeout: time.Hour,
	}, WithOnError(func(err error) { pollErr = err }))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p.Poll(ctx)

	is.True(errors.Is(pollErr, context.Canceled)) // the cancellation reaches the Getter
	is.Equal(p.Current(), "initial")              // the value stays
}

func TestPollerKeepsValueOnError(t *testing.T) {
	is := is.New(t)

	wantErr := errors.New("source down")
	var pollErr error
	first := true
	p := New(nil, "initial", func(context.Context) (string, error) {
		if first {
			first = false
			return "fresh", nil
		}
		return "", wantErr
	}, Config{
		Interval: time.Hour, // the test drives the polls
	}, WithOnError(func(err error) { pollErr = err }))

	p.Poll(t.Context())
	is.Equal(p.Current(), "fresh") // a successful poll updates the value

	p.Poll(t.Context())
	is.Equal(p.Current(), "fresh")       // a failed poll leaves it
	is.True(errors.Is(pollErr, wantErr)) // and reaches the WithOnError handler
}

func TestPollerRecoversPanic(t *testing.T) {
	is := is.New(t)

	panics := 0
	p := New(nil, 0, func(context.Context) (int, error) {
		panic("boom")
	}, Config{Interval: time.Hour}, WithOnPanic(func(string, string) {
		panics++
	}))

	p.Poll(t.Context()) // must not propagate the panic

	is.Equal(panics, 1)      // the WithOnPanic handler fires once
	is.Equal(p.Current(), 0) // the value stays
}

// The polls of Start run suppressed: they export no traces.
func TestPollerStartSuppressesPolls(t *testing.T) {
	is := is.New(t)

	seen := make(chan bool, 1)
	p := New(nil, false, func(ctx context.Context) (bool, error) {
		select {
		case seen <- otel.IsSuppressed(ctx):
		default:
		}
		return true, nil
	}, Config{Name: "test", Interval: time.Hour})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = p.Start(ctx) }()

	is.True(<-seen) // the first poll of Start is suppressed
}
