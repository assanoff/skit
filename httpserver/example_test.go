package httpserver_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/httpserver"
	"github.com/assanoff/skit/worker"
)

// hello is the handler the server serves: a router in a service.
func hello() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "hello")
	})
	return mux
}

// A service runs the server in a worker group, next to the gRPC and status
// servers: the group starts them and drains them all on SIGINT or SIGTERM.
func Example() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	srv := httpserver.New(httpserver.Config{
		Name:   "rest-server",
		Addr:   ":8080",
		Logger: log,
	}, hello())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	grp := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	grp.Add(srv)
	if err := grp.Run(ctx); err != nil {
		log.ErrorContext(ctx, "run", "err", err)
	}
}

// Serve takes a listener of the caller, here an ephemeral port; Stop drains
// the requests in flight and Serve returns.
func ExampleServer_Serve() {
	srv := httpserver.New(httpserver.Config{Logger: slog.New(slog.DiscardHandler)}, hello())
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println(err)
		return
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx,
		http.MethodGet, "http://"+lis.Addr().String()+"/hello", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	fmt.Println(resp.StatusCode, string(body), err)

	fmt.Println(srv.Name(), srv.Stop(ctx), <-served)
	// Output:
	// 200 hello <nil>
	// http-server <nil> <nil>
}
