package grpcgateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// pingRegistrar adds a static GET /ping route via the mux's HandlePath, which
// lets us exercise the wiring (mux built, registrars applied, served as an
// http.Handler) without generated proto code.
func pingRegistrar(_ context.Context, mux *runtime.ServeMux, _ *grpc.ClientConn) error {
	return mux.HandlePath(http.MethodGet, "/ping",
		func(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("pong"))
		})
}

func TestNewServesRegisteredRoutes(t *testing.T) {
	gw, err := New(context.Background(), Config{Endpoint: "localhost:0"}, pingRegistrar, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = gw.Close() })

	srv := httptest.NewServer(gw)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/ping")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "pong" {
		t.Fatalf("GET /ping = (%d, %q), want (200, %q)", resp.StatusCode, body, "pong")
	}
}

func TestNewDialsAndOwnsConn(t *testing.T) {
	gw, err := New(context.Background(), Config{Endpoint: "localhost:0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !gw.ownConn || gw.conn == nil {
		t.Fatal("expected New to dial and own the connection")
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNewWithClientConnIsNotOwned(t *testing.T) {
	conn, err := grpc.NewClient("localhost:0", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()

	gw, err := New(context.Background(), Config{ClientConn: conn})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if gw.ownConn {
		t.Fatal("supplied ClientConn must not be owned by the Gateway")
	}
	// Close is a no-op and must leave the caller's conn usable.
	if err := gw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if conn.GetState().String() == "SHUTDOWN" {
		t.Fatal("Close must not shut down a caller-supplied connection")
	}
}

func TestNewRegistrarErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := New(context.Background(), Config{Endpoint: "localhost:0"},
		func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapped %v", err, sentinel)
	}
}
