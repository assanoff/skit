package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewAppliesDefaults(t *testing.T) {
	s := New(Config{Addr: ":0"}, http.NewServeMux())
	if s.Name() != defaultName {
		t.Fatalf("name = %q, want %q", s.Name(), defaultName)
	}
	if s.Addr() != ":0" {
		t.Fatalf("addr = %q", s.Addr())
	}
	if s.shutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("shutdownTimeout = %s, want %s", s.shutdownTimeout, defaultShutdownTimeout)
	}
	if s.server.ReadHeaderTimeout != defaultReadHeader {
		t.Fatalf("readHeaderTimeout = %s, want %s", s.server.ReadHeaderTimeout, defaultReadHeader)
	}
	if s.server.IdleTimeout != defaultIdleTimeout {
		t.Fatalf("idleTimeout = %s, want %s", s.server.IdleTimeout, defaultIdleTimeout)
	}
}

func TestNewCustomNameAndDisabledReadHeader(t *testing.T) {
	s := New(Config{Name: "status-server", ReadHeaderTimeout: -1}, http.NewServeMux())
	if s.Name() != "status-server" {
		t.Fatalf("name = %q", s.Name())
	}
	if s.server.ReadHeaderTimeout != 0 {
		t.Fatalf("readHeaderTimeout = %s, want 0 (disabled)", s.server.ReadHeaderTimeout)
	}
}

func TestNewNegativeIdleTimeoutLeavesItToNetHTTP(t *testing.T) {
	s := New(Config{IdleTimeout: -1}, http.NewServeMux())
	if s.server.IdleTimeout != 0 {
		t.Fatalf("idleTimeout = %s, want 0 (net/http default)", s.server.IdleTimeout)
	}
}

func TestServeAndGracefulStop(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := New(Config{Name: "test", Logger: quietLogger()}, http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) },
	))

	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(lis) }()

	resp, err := http.Get("http://" + lis.Addr().String() + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusTeapot)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("serve returned error: %v", err)
	}
}

// Start reports an address it cannot bind instead of serving.
func TestStartAddrInUse(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	s := New(Config{Addr: lis.Addr().String(), Logger: quietLogger()}, http.NotFoundHandler())

	errCh := make(chan error, 1)
	go func() { errCh <- s.Start(context.Background()) }()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Start on an address in use = nil, want an error")
		}
	case <-time.After(5 * time.Second):
		_ = s.Stop(context.Background())
		t.Fatal("Start is serving on an address in use")
	}
}

// A handler still running when the shutdown wait runs out does not keep the
// server alive: Stop reports the timeout and closes its connection.
func TestStopClosesStuckConnections(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	entered, released := make(chan struct{}), make(chan struct{})
	s := New(Config{Logger: quietLogger()}, http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done() // canceled once the connection is closed
			close(released)
		},
	))
	go func() { _ = s.Serve(lis) }()
	go func() {
		resp, err := http.Get("http://" + lis.Addr().String() + "/")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Stop(ctx); err == nil {
		t.Fatal("Stop with a stuck handler = nil, want the shutdown timeout")
	}

	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the stuck connection was not closed")
	}
}
