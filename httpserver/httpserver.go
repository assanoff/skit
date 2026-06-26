package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const (
	defaultName            = "http-server"
	defaultReadHeader      = 5 * time.Second
	defaultShutdownTimeout = 10 * time.Second
)

// Config configures a Server. Only Addr is required; every other field falls
// back to a safe default.
type Config struct {
	// Addr is the listen address, e.g. ":8080".
	Addr string
	// Name identifies the server to the supervisor and in logs (default
	// "http-server"). Set it so REST/gateway/status read distinctly.
	Name string
	// ReadHeaderTimeout bounds reading request headers (default 5s) to guard
	// against Slowloris-style stalls. Set a negative duration to disable it.
	ReadHeaderTimeout time.Duration
	// ReadTimeout, WriteTimeout and IdleTimeout map to the http.Server fields of
	// the same name; zero leaves each at the net/http default (no limit).
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	// ShutdownTimeout bounds graceful shutdown when Stop's context carries no
	// deadline (default 10s).
	ShutdownTimeout time.Duration
	// Logger receives start/stop lines; defaults to slog.Default().
	Logger *slog.Logger
}

// Server serves an http.Handler on its own listener and implements
// worker.Runnable (Start/Stop/Name). It is the standard-library-idiomatic core
// of the server kit: REST, the gRPC-gateway and the status server are all just
// this brick wrapped around a different http.Handler, so they compose in a
// single worker.Group.
type Server struct {
	server          *http.Server
	log             *slog.Logger
	name            string
	shutdownTimeout time.Duration
}

// New wraps h in an *http.Server configured by cfg. The server is not listening
// until Start (or Serve) is called.
func New(cfg Config, h http.Handler) *Server {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	name := cfg.Name
	if name == "" {
		name = defaultName
	}
	readHeader := cfg.ReadHeaderTimeout
	switch {
	case readHeader < 0:
		readHeader = 0 // explicitly disabled
	case readHeader == 0:
		readHeader = defaultReadHeader
	}
	shutdown := cfg.ShutdownTimeout
	if shutdown == 0 {
		shutdown = defaultShutdownTimeout
	}
	return &Server{
		server: &http.Server{
			Addr:              cfg.Addr,
			Handler:           h,
			ReadHeaderTimeout: readHeader,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
		log:             log,
		name:            name,
		shutdownTimeout: shutdown,
	}
}

// Name identifies the runnable to the supervisor.
func (s *Server) Name() string { return s.name }

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.server.Addr }

// Start binds a listener on the configured address and serves until Stop.
// ErrServerClosed is treated as a clean shutdown.
func (s *Server) Start(ctx context.Context) error {
	s.log.InfoContext(ctx, "http server listening", "name", s.name, "addr", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Serve serves on the provided listener until Stop. It is useful for tests (an
// ephemeral localhost:0 listener) and for callers that manage their own
// listener. ErrServerClosed is treated as a clean shutdown.
func (s *Server) Serve(lis net.Listener) error {
	if err := s.server.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop gracefully drains in-flight requests, bounding the wait by
// ShutdownTimeout when ctx carries no deadline.
func (s *Server) Stop(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
	}
	s.log.InfoContext(ctx, "http server shutting down", "name", s.name)
	return s.server.Shutdown(ctx)
}
