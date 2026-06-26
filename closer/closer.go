package closer

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultSignals is the default list of signals NewWithWait listens for.
var DefaultSignals = []os.Signal{
	syscall.SIGINT,
	syscall.SIGTERM,
	syscall.SIGQUIT,
	syscall.SIGHUP,
}

// handler is a function that can be added to Closer.
type handler func() error

// Closer manages graceful shutdown of resources, executing registered cleanup
// handlers in LIFO (Last-In-First-Out) order.
type Closer struct {
	wg     sync.WaitGroup
	mu     sync.Mutex
	once   sync.Once
	fns    []handler
	closed atomic.Uint32
}

// c is the global instance for use throughout the application.
var c = New()

// New creates a new Closer.
func New() *Closer {
	return &Closer{}
}

// NewWithWait creates a Closer and starts a goroutine that blocks until one of
// the given signals arrives (DefaultSignals when none are passed). Wait blocks
// until that happens.
func NewWithWait(sigs ...os.Signal) *Closer {
	cl := &Closer{}

	if len(sigs) == 0 {
		sigs = DefaultSignals
	}

	cl.wg.Go(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, sigs...)
		<-ch
		signal.Stop(ch)
	})

	return cl
}

// Add adds a shutdown function. Handlers run in LIFO order on Close/CloseSync.
// A nil fn is ignored.
func (c *Closer) Add(fn func() error) { c.add(fn) }

// AddNamed adds a shutdown function with a name used in logging.
func (c *Closer) AddNamed(name string, fn func() error) { c.addNamed(name, fn) }

// Close executes the handlers asynchronously (concurrently). It does NOT
// guarantee LIFO order; use CloseSync when order matters.
func (c *Closer) Close() error { return c.close() }

// CloseSync executes the handlers sequentially in LIFO order.
func (c *Closer) CloseSync() error { return c.closeSync() }

// Wait blocks until the signal goroutine (from NewWithWait) returns.
func (c *Closer) Wait() { c.wg.Wait() }

// --- package-level helpers operating on the global instance ---

// Add adds a shutdown function to the global closer.
func Add(fn func() error) { c.add(fn) }

// AddNamed adds a named shutdown function to the global closer.
func AddNamed(name string, fn func() error) { c.addNamed(name, fn) }

// Close closes all functions in the global closer concurrently (no order guarantee).
func Close() error { return c.close() }

// CloseSync closes all functions in the global closer sequentially in LIFO order.
func CloseSync() error { return c.closeSync() }

func (c *Closer) copyAndReverseHandlers() []handler {
	c.mu.Lock()
	defer c.mu.Unlock()

	slog.Debug("preparing to close handlers", slog.Int("count", len(c.fns)))

	funcs := make([]handler, len(c.fns))
	for i, fn := range c.fns {
		funcs[len(c.fns)-1-i] = fn
	}

	c.fns = nil
	c.closed.Store(1)

	return funcs
}

func (c *Closer) add(fn handler) {
	if fn == nil {
		slog.Warn("attempt to add nil handler")
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() > 0 {
		slog.Warn("attempt to add handler after close")
		return
	}
	c.fns = append(c.fns, fn)
	slog.Debug("handler registered", slog.Int("total", len(c.fns)))
}

func (c *Closer) addNamed(name string, fn func() error) {
	c.add(func() error {
		start := time.Now()
		slog.Info("closing", slog.String("name", name))
		err := fn()
		if err != nil {
			slog.Error("failed to close", slog.String("name", name), slog.String("error", err.Error()))
		} else {
			slog.Info("closed", slog.String("name", name), slog.Duration("duration", time.Since(start)))
		}
		return err
	})
}

func (c *Closer) close() error {
	var errs []error

	c.once.Do(func() {
		funcs := c.copyAndReverseHandlers()
		if len(funcs) == 0 {
			slog.Debug("no handlers to execute")
			return
		}

		errsCh := make(chan error, len(funcs))
		var wg sync.WaitGroup

		for i := range funcs {
			fn := funcs[i]
			if fn == nil {
				continue
			}
			wg.Add(1)
			go func(fn handler) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						errsCh <- fmt.Errorf("panic recovered in closer: %v", r)
						slog.Error("panic recovered", slog.String("error", fmt.Sprintf("%v", r)))
					}
				}()
				if err := fn(); err != nil {
					errsCh <- err
				}
			}(fn)
		}

		go func() {
			defer close(errsCh)
			wg.Wait()
		}()

		for err := range errsCh {
			errs = append(errs, err)
		}
	})

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (c *Closer) closeSync() error {
	var errs []error

	c.once.Do(func() {
		funcs := c.copyAndReverseHandlers()
		if len(funcs) == 0 {
			slog.Debug("no handlers to execute")
			return
		}

		for i := range funcs {
			fn := funcs[i]
			if fn == nil {
				continue
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						errs = append(errs, fmt.Errorf("panic recovered in closer: %v", r))
						slog.Error("panic recovered", slog.String("error", fmt.Sprintf("%v", r)))
					}
				}()
				if err := fn(); err != nil {
					errs = append(errs, err)
				}
			}()
		}
	})

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
