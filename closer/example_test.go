package closer_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/assanoff/skit/closer"
)

// CloseSync runs the teardowns in reverse: the server stops before the pool
// it uses. Errors are joined, and a second close does nothing.
func Example() {
	cl := closer.New()
	cl.AddNamed("db", func() error {
		fmt.Println("close db")
		return nil
	})
	cl.AddNamed("cache", func() error {
		fmt.Println("close cache")
		return errors.New("cache: connection reset")
	})
	cl.AddNamed("http", func() error {
		fmt.Println("stop http")
		return nil
	})

	fmt.Println("err:", cl.CloseSync())
	fmt.Println("again:", cl.CloseSync())
	// Output:
	// stop http
	// close cache
	// close db
	// err: cache: connection reset
	// again: <nil>
}

// Close runs independent teardowns at once, in no order, and joins their
// errors.
func ExampleCloser_Close() {
	cl := closer.New()
	var closed atomic.Int32
	for range 3 {
		cl.Add(func() error {
			closed.Add(1)
			return nil
		})
	}
	cl.Add(func() error { return errors.New("broker: timeout") })

	fmt.Println("err:", cl.Close())
	fmt.Println("closed:", closed.Load())
	// Output:
	// err: broker: timeout
	// closed: 3
}

// A service registers its teardowns as it builds the resources, waits for
// SIGINT or SIGTERM, then closes them in reverse.
func ExampleNewWithWait() {
	cl := closer.NewWithWait() // DefaultSignals

	srv := &http.Server{Addr: ":8080"}
	go func() { _ = srv.ListenAndServe() }()
	cl.AddNamed("http", func() error {
		return srv.Shutdown(context.Background())
	})

	cl.Wait()
	if err := cl.CloseSync(); err != nil {
		slog.Error("shutdown", "err", err)
	}
}
