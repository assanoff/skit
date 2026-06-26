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
