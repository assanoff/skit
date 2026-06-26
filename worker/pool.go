package worker

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
)

// JobFn is a unit of work submitted to a Pool.
type JobFn func(ctx context.Context)

// ErrPoolShutdown is returned by Pool.Submit once Shutdown has been called.
var ErrPoolShutdown = errors.New("worker: pool is shutting down")

// Pool runs one-shot jobs with a bounded degree of concurrency. Submit blocks
// until a slot is free (or ctx is canceled / the pool is shutting down), then
// launches the job on its own goroutine. Shutdown cancels every in-flight job
// and waits for them to return.
//
// Use Pool for bounded fan-out of discrete tasks. For recurring work use Loop;
// for reliable claim/process/ack pipelines use Processor.
type Pool struct {
	sem        chan struct{}
	wg         sync.WaitGroup
	mu         sync.Mutex
	running    map[string]context.CancelFunc
	isShutdown chan struct{}
}

// NewPool constructs a Pool that runs at most maxConcurrent jobs at once.
// maxConcurrent is clamped to a minimum of 1.
func NewPool(maxConcurrent int) *Pool {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Pool{
		sem:        make(chan struct{}, maxConcurrent),
		running:    make(map[string]context.CancelFunc),
		isShutdown: make(chan struct{}),
	}
}

// Running reports the number of jobs currently executing.
func (p *Pool) Running() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.running)
}

// Submit blocks until a slot is free, then launches job on its own goroutine.
// The job receives a context that is canceled either when the submission ctx is
// canceled or when Shutdown is called, whichever comes first. It returns a key
// that can be passed to Cancel to stop that single job early.
func (p *Pool) Submit(ctx context.Context, job JobFn) (string, error) {
	// Prefer the shutdown signal so a draining pool rejects new work promptly.
	select {
	case <-p.isShutdown:
		return "", ErrPoolShutdown
	default:
	}

	select {
	case <-p.isShutdown:
		return "", ErrPoolShutdown
	case <-ctx.Done():
		return "", ctx.Err()
	case p.sem <- struct{}{}:
	}

	// Detach from the submission ctx so a single Submit caller returning does not
	// cancel the job, but keep any deadline it carried. Cancel/Shutdown drive the
	// job's lifetime instead.
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	if deadline, ok := ctx.Deadline(); ok {
		jobCtx, cancel = context.WithDeadline(context.WithoutCancel(ctx), deadline)
	}

	key := uuid.NewString()
	p.track(key, cancel)

	p.wg.Add(1)
	go func() {
		// Release the semaphore in its own defer so a panic in the cleanup defer
		// still frees the slot.
		defer func() { <-p.sem }()
		defer func() {
			cancel()
			p.untrack(key)
			p.wg.Done()
		}()
		job(jobCtx)
	}()

	return key, nil
}

// Cancel stops a single in-flight job by its key. Unknown keys (already
// finished or never issued) are a no-op.
func (p *Pool) Cancel(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cancel, ok := p.running[key]; ok {
		cancel()
	}
}

// Shutdown signals the pool to stop accepting work, cancels every in-flight
// job, and waits for them to return or until ctx is canceled. It is idempotent.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	select {
	case <-p.isShutdown:
	default:
		close(p.isShutdown)
	}
	for _, cancel := range p.running {
		cancel()
	}
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pool) track(key string, cancel context.CancelFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running[key] = cancel
}

func (p *Pool) untrack(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.running, key)
}
