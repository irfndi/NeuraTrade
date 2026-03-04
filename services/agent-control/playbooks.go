// Package playbooks provides automated self-healing playbook system.
package agentcontrol

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PlaybookFunc is the function signature for playbook execution.
type PlaybookFunc func(ctx context.Context, event any) error

// Playbook represents an automated response playbook.
type Playbook struct {
	Name        string
	Description string
	Execute     PlaybookFunc
	Timeout     time.Duration
}

// Registry manages playbook registration and retrieval.
type Registry struct {
	mu        sync.RWMutex
	playbooks map[string]*Playbook
}

// NewRegistry creates a new playbook registry.
func NewRegistry() *Registry {
	return &Registry{
		playbooks: make(map[string]*Playbook),
	}
}

// Register adds a playbook to the registry.
func (r *Registry) Register(name string, playbook Playbook) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Set default timeout if not specified
	if playbook.Timeout <= 0 {
		playbook.Timeout = 30 * time.Second
	}

	r.playbooks[name] = &playbook
}

// Get retrieves a playbook by name.
func (r *Registry) Get(name string) (*Playbook, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	playbook, ok := r.playbooks[name]
	return playbook, ok
}

// List returns all registered playbooks.
func (r *Registry) List() []*Playbook {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Playbook, 0, len(r.playbooks))
	for _, playbook := range r.playbooks {
		result = append(result, playbook)
	}
	return result
}

// Execute runs a playbook with timeout handling.
func (r *Registry) Execute(ctx context.Context, name string, event any) error {
	playbook, ok := r.Get(name)
	if !ok {
		return fmt.Errorf("playbook not found: %s", name)
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, playbook.Timeout)
	defer cancel()

	// Execute playbook
	done := make(chan error, 1)
	go func() {
		done <- playbook.Execute(execCtx, event)
	}()

	select {
	case err := <-done:
		return err
	case <-execCtx.Done():
		return fmt.Errorf("playbook execution timed out after %v", playbook.Timeout)
	}
}

// Count returns the number of registered playbooks.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.playbooks)
}
