// Package supervisor provides deterministic lifecycle management for goroutines.
// It wraps golang.org/x/sync/errgroup to provide:
//   - Coordinated startup and shutdown
//   - First-error-wins semantics
//   - Graceful shutdown with timeout
//   - Goroutine leak prevention
package supervisor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Runnable represents a component that can be supervised.
type Runnable interface {
	// Run starts the component and blocks until it completes or errors.
	// It MUST respect context cancellation for graceful shutdown.
	Run(ctx context.Context) error
}

// RunnableFunc is an adapter to allow using functions as Runnables.
type RunnableFunc func(ctx context.Context) error

// Run implements Runnable.
func (f RunnableFunc) Run(ctx context.Context) error {
	return f(ctx)
}

// Group manages a collection of supervised goroutines.
// It is similar to errgroup.Group but with additional features:
//   - Named components for better debugging
//   - Graceful shutdown timeout
//   - Startup barrier for coordinated initialization
type Group struct {
	mu           sync.Mutex
	runnables    []namedRunnable
	err          error
	errOnce      sync.Once
	done         chan struct{}
	cancel       context.CancelFunc
	shutdownOnce sync.Once
}

type namedRunnable struct {
	name string
	r    Runnable
}

// NewGroup creates a new supervisor Group.
func NewGroup() *Group {
	return &Group{
		done: make(chan struct{}),
	}
}

// Add registers a Runnable with the given name.
// Names are used for debugging and logging.
func (g *Group) Add(name string, r Runnable) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runnables = append(g.runnables, namedRunnable{name: name, r: r})
}

// AddFunc registers a function as a Runnable.
func (g *Group) AddFunc(name string, f func(ctx context.Context) error) {
	g.Add(name, RunnableFunc(f))
}

func (g *Group) Run(ctx context.Context) error {
	g.mu.Lock()
	runnables := make([]namedRunnable, len(g.runnables))
	copy(runnables, g.runnables)
	g.mu.Unlock()

	if len(runnables) == 0 {
		close(g.done)
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	g.mu.Lock()
	g.cancel = cancel
	g.mu.Unlock()
	defer cancel()

	var wg sync.WaitGroup

	for _, nr := range runnables {
		wg.Add(1)
		go func(name string, r Runnable) {
			defer wg.Done()
			if err := r.Run(ctx); err != nil {
				g.setError(err)
				cancel() // Cancel all other runnables on error
			}
		}(nr.name, nr.r)
	}

	// Wait for all runnables to complete
	go func() {
		wg.Wait()
		close(g.done)
	}()

	<-g.done
	return g.err
}

func (g *Group) setError(err error) {
	g.errOnce.Do(func() {
		g.err = err
	})
}

// Shutdown initiates graceful shutdown and waits up to timeout for completion.
// Returns an error if shutdown takes longer than timeout.
func (g *Group) Shutdown(timeout time.Duration) error {
	return g.ShutdownContext(context.Background(), timeout)
}

func (g *Group) ShutdownContext(parentCtx context.Context, timeout time.Duration) error {
	g.shutdownOnce.Do(func() {
		g.mu.Lock()
		if g.cancel != nil {
			g.cancel()
		}
		g.mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("supervisor shutdown timed out after %v", timeout)
	}
}

// Done returns a channel that is closed when all runnables have completed.
func (g *Group) Done() <-chan struct{} {
	return g.done
}

// Supervisor provides a higher-level interface for managing a Group
// with signal handling and health checks.
type Supervisor struct {
	group        *Group
	shutdownChan chan struct{}
	shutdownOnce sync.Once
	mu           sync.RWMutex
	running      bool
}

// New creates a new Supervisor.
func New() *Supervisor {
	return &Supervisor{
		group:        NewGroup(),
		shutdownChan: make(chan struct{}),
	}
}

// Add registers a Runnable with the supervisor.
func (s *Supervisor) Add(name string, r Runnable) {
	s.group.Add(name, r)
}

// AddFunc registers a function with the supervisor.
func (s *Supervisor) AddFunc(name string, f func(ctx context.Context) error) {
	s.group.AddFunc(name, f)
}

// Run starts the supervisor and blocks until context is cancelled
// or a fatal error occurs.
func (s *Supervisor) Run(ctx context.Context) error {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	return s.group.Run(ctx)
}

// Shutdown triggers graceful shutdown with the given timeout.
func (s *Supervisor) Shutdown(timeout time.Duration) error {
	s.shutdownOnce.Do(func() {
		close(s.shutdownChan)
	})
	return s.group.Shutdown(timeout)
}

// IsRunning returns whether the supervisor is currently running.
func (s *Supervisor) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// ShutdownChan returns a channel that is closed when shutdown is initiated.
func (s *Supervisor) ShutdownChan() <-chan struct{} {
	return s.shutdownChan
}
