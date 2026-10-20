package poller_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/assanoff/skit/poller"
	"github.com/assanoff/skit/worker"
)

// A worker.Group supervises the poller next to the rest of the process: Start
// polls at once, then every Interval, until the group shuts down. Hot paths
// read the cached value with Current.
func Example() {
	// fetched only lets the example wait for the first poll.
	fetched := make(chan struct{}, 1)
	p := poller.New(nil, "default", func(context.Context) (string, error) {
		select {
		case fetched <- struct{}{}:
		default:
		}
		return "remote", nil
	}, poller.Config{Name: "flags", Interval: time.Minute})

	g := worker.NewGroup(nil, worker.GroupConfig{ShutdownTimeout: 5 * time.Second})
	g.Add(p)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()

	<-fetched
	cancel() // the process shuts down
	if err := <-done; err != nil {
		log.Fatal(err)
	}
	fmt.Println(p.Current())
	// Output: remote
}

// Poll fetches once, synchronously: Current returns the initial value until
// the first successful poll.
func ExamplePoller_Poll() {
	p := poller.New(nil, 0.0, func(context.Context) (float64, error) {
		return 470.5, nil
	}, poller.Config{Name: "usd-kzt", Interval: time.Minute})

	fmt.Println(p.Current())
	p.Poll(context.Background())
	fmt.Println(p.Current())
	// Output:
	// 0
	// 470.5
}
