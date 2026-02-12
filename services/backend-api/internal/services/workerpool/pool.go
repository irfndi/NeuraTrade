// Package workerpool provides a managed goroutine pool with configurable concurrency limits.
package workerpool

import (
	"context"
	"fmt"
	"sync"
)

// Pool manages a fixed number of worker goroutines that execute submitted tasks.
type Pool struct {
	workers    int
	taskQueue  chan Task
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	running    bool
	dropOnFull bool
}

// Task represents a unit of work to be executed by the pool.
type Task struct {
	ID      string
	Execute func() error
}

// Result contains the outcome of a task execution.
type Result struct {
	TaskID string
	Error  error
}

// Config defines pool configuration options.
type Config struct {
	Workers    int
	QueueSize  int
	DropOnFull bool
}

// DefaultConfig returns sensible defaults for pool configuration.
func DefaultConfig() Config {
	return Config{
		Workers:    10,
		QueueSize:  100,
		DropOnFull: false,
	}
}

// New creates a new worker pool with the specified configuration.
func New(cfg Config) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		workers:    cfg.Workers,
		taskQueue:  make(chan Task, cfg.QueueSize),
		ctx:        ctx,
		cancel:     cancel,
		running:    false,
		dropOnFull: cfg.DropOnFull,
	}
}

// Start initializes the worker goroutines.
func (p *Pool) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("pool already running")
	}

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	p.running = true
	return nil
}

// Stop gracefully shuts down the pool, waiting for queued tasks to complete.
func (p *Pool) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return fmt.Errorf("pool not running")
	}
	p.running = false
	p.mu.Unlock()

	p.cancel()
	close(p.taskQueue)
	p.wg.Wait()

	return nil
}

// Submit adds a task to the pool for execution.
func (p *Pool) Submit(task Task) error {
	p.mu.RLock()
	if !p.running {
		p.mu.RUnlock()
		return fmt.Errorf("pool not running")
	}
	p.mu.RUnlock()

	if p.dropOnFull {
		select {
		case p.taskQueue <- task:
			return nil
		default:
			return fmt.Errorf("task queue full, task dropped")
		}
	}

	select {
	case p.taskQueue <- task:
		return nil
	case <-p.ctx.Done():
		return fmt.Errorf("pool shutting down")
	}
}

// SubmitAsync submits a task and returns a channel for the result.
func (p *Pool) SubmitAsync(task Task) (<-chan Result, error) {
	resultCh := make(chan Result, 1)

	wrappedTask := Task{
		ID: task.ID,
		Execute: func() error {
			err := task.Execute()
			resultCh <- Result{TaskID: task.ID, Error: err}
			close(resultCh)
			return err
		},
	}

	if err := p.Submit(wrappedTask); err != nil {
		close(resultCh)
		return nil, err
	}

	return resultCh, nil
}

// GetQueueDepth returns the current number of tasks in the queue.
func (p *Pool) GetQueueDepth() int {
	return len(p.taskQueue)
}

// GetQueueCapacity returns the maximum capacity of the task queue.
func (p *Pool) GetQueueCapacity() int {
	return cap(p.taskQueue)
}

// IsRunning returns true if the pool is active.
func (p *Pool) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case task, ok := <-p.taskQueue:
			if !ok {
				return
			}
			_ = task.Execute()
		}
	}
}

// PoolManager manages multiple named worker pools.
type PoolManager struct {
	mu    sync.RWMutex
	pools map[string]*Pool
}

// NewPoolManager creates a new pool manager.
func NewPoolManager() *PoolManager {
	return &PoolManager{
		pools: make(map[string]*Pool),
	}
}

// CreatePool creates and starts a new named pool.
func (pm *PoolManager) CreatePool(name string, cfg Config) (*Pool, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.pools[name]; exists {
		return nil, fmt.Errorf("pool %s already exists", name)
	}

	pool := New(cfg)
	if err := pool.Start(); err != nil {
		return nil, fmt.Errorf("failed to start pool %s: %w", name, err)
	}

	pm.pools[name] = pool
	return pool, nil
}

// GetPool returns an existing pool by name.
func (pm *PoolManager) GetPool(name string) (*Pool, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	pool, exists := pm.pools[name]
	return pool, exists
}

// StopPool stops and removes a specific pool.
func (pm *PoolManager) StopPool(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pool, exists := pm.pools[name]
	if !exists {
		return fmt.Errorf("pool %s not found", name)
	}

	if err := pool.Stop(); err != nil {
		return fmt.Errorf("failed to stop pool %s: %w", name, err)
	}

	delete(pm.pools, name)
	return nil
}

// StopAll stops all managed pools.
func (pm *PoolManager) StopAll() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var errs []error
	for name, pool := range pm.pools {
		if err := pool.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop pool %s: %w", name, err))
		}
		delete(pm.pools, name)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping pools: %v", errs)
	}

	return nil
}

// GetPoolNames returns a list of all pool names.
func (pm *PoolManager) GetPoolNames() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.pools))
	for name := range pm.pools {
		names = append(names, name)
	}

	return names
}

// GetPoolStats returns statistics for all pools.
func (pm *PoolManager) GetPoolStats() map[string]PoolStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[string]PoolStats)
	for name, pool := range pm.pools {
		stats[name] = PoolStats{
			Running:       pool.IsRunning(),
			QueueDepth:    pool.GetQueueDepth(),
			QueueCapacity: pool.GetQueueCapacity(),
		}
	}

	return stats
}

// PoolStats contains runtime statistics for a pool.
type PoolStats struct {
	Running       bool
	QueueDepth    int
	QueueCapacity int
}
