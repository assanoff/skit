package worker_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/assanoff/skit/worker"
)

// A Processor drains a source in a paced Loop that a Group supervises. Here the
// source is a slice: the first tick claims a full batch, so the next runs at
// once; a tick that finds the source empty stops the example.
func Example() {
	type job struct {
		id   int
		name string
	}
	errBadInput := errors.New("bad input")
	pending := []job{{1, "ann"}, {2, "bob"}, {3, ""}}
	ctx, stop := context.WithCancel(context.Background())

	steps := worker.Steps[job]{
		Claim: func(_ context.Context, _ time.Time, limit int) ([]job, error) {
			n := min(limit, len(pending))
			batch := pending[:n]
			pending = pending[n:]
			if n == 0 {
				stop()
			}
			return batch, nil
		},
		Handle: func(_ context.Context, j job) error {
			if j.name == "" {
				return fmt.Errorf("job %d: %w", j.id, errBadInput)
			}
			fmt.Println("hello,", j.name)
			return nil
		},
		MarkDone: func(_ context.Context, j job, _ time.Time) error {
			fmt.Println("done", j.id)
			return nil
		},
		MarkFailed: func(_ context.Context, j job, msg string, terminal bool, _ time.Time) error {
			fmt.Printf("failed %d: %s, terminal %t\n", j.id, msg, terminal)
			return nil
		},
	}
	proc := worker.NewProcessor(nil, steps, worker.ProcessorConfig{
		Name:      "greetings",
		BatchSize: 2,
		IsTerminal: func(err error) bool {
			return errors.Is(err, errBadInput)
		},
	})
	loop := worker.NewPacedLoop(nil, worker.LoopConfig{
		Name:               "greetings",
		Interval:           10 * time.Millisecond, // the wait after an idle tick
		ImmediateFirstTick: true,
	}, proc.PacedTick())

	g := worker.NewGroup(nil, worker.GroupConfig{ShutdownTimeout: time.Second})
	g.Add(loop)
	fmt.Println("run:", g.Run(ctx))
	// Output:
	// hello, ann
	// done 1
	// hello, bob
	// done 2
	// failed 3: job 3: bad input, terminal true
	// run: <nil>
}

// The first Runnable to fail tears the Group down: the others are stopped in
// reverse order of Add, and Run returns the error.
func ExampleGroup() {
	stopFn := func(name string) func(context.Context) error {
		return func(context.Context) error {
			fmt.Println("stop", name)
			return nil
		}
	}

	g := worker.NewGroup(nil, worker.GroupConfig{ShutdownTimeout: time.Second})
	g.Add(
		// No StartFn: the Runnable runs until the Group stops.
		worker.RunnableFunc{NameValue: "http", StopFn: stopFn("http")},
		worker.RunnableFunc{NameValue: "consumer", StopFn: stopFn("consumer")},
		worker.RunnableFunc{
			NameValue: "import",
			StartFn: func(context.Context) error {
				return errors.New("import: source is down")
			},
		},
	)
	fmt.Println("run:", g.Run(context.Background()))
	// Output:
	// stop consumer
	// stop http
	// run: import: source is down
}

// An error of a tick is logged, and the Loop ticks on until its context is
// canceled: here by its third tick.
func ExampleNewLoop() {
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	ctx, stop := context.WithCancel(context.Background())

	n := 0
	loop := worker.NewLoop(slog.New(h), worker.LoopConfig{
		Name:               "report",
		Interval:           10 * time.Millisecond,
		ImmediateFirstTick: true,
	}, func(context.Context) error {
		n++
		switch n {
		case 1:
			return errors.New("report: source is down")
		case 3:
			stop()
		}
		return nil
	})
	fmt.Println("start:", loop.Start(ctx))
	// Output:
	// level=ERROR msg="worker tick error" worker=report err="report: source is down"
	// start: context canceled
}

// A Pool runs at most MaxConcurrent jobs at once. Submit returns the key that
// Cancel takes to stop that one job.
func ExamplePool() {
	ctx := context.Background()
	pool := worker.NewPool(worker.PoolConfig{MaxConcurrent: 2})

	var (
		mu   sync.Mutex
		sent []string
		wg   sync.WaitGroup
	)
	for _, to := range []string{"eve", "ann", "bob"} {
		wg.Add(1)
		_, err := pool.Submit(ctx, func(context.Context) {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			sent = append(sent, to)
		})
		if err != nil {
			fmt.Println("submit:", err)
			return
		}
	}
	wg.Wait()
	slices.Sort(sent)
	fmt.Println("sent:", sent)

	canceled := make(chan error)
	key, err := pool.Submit(ctx, func(ctx context.Context) {
		<-ctx.Done()
		canceled <- ctx.Err()
	})
	if err != nil {
		fmt.Println("submit:", err)
		return
	}
	pool.Cancel(key)
	fmt.Println("job:", <-canceled)
	fmt.Println("shutdown:", pool.Shutdown(ctx))
	// Output:
	// sent: [ann bob eve]
	// job: context canceled
	// shutdown: <nil>
}
