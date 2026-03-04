// Package agentcontrol provides the agent runtime orchestration.
package agentcontrol

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AgentRuntimeConfig holds configuration for the agent runtime.
type AgentRuntimeConfig struct {
	AuditLogger      *Logger
	BackendClient    *BackendClient
	EventIngestor    *Ingestor
	PolicyEngine     *Engine
	PlaybookRegistry *Registry
}

// AgentRuntime manages the agent lifecycle and event processing.
type AgentRuntime struct {
	config       AgentRuntimeConfig
	mu           sync.Mutex
	running      bool
	shutdownChan chan struct{}
	eventChan    <-chan Event
	processingWg sync.WaitGroup
}

// NewAgentRuntime creates a new agent runtime.
func NewAgentRuntime(config AgentRuntimeConfig) *AgentRuntime {
	return &AgentRuntime{
		config:       config,
		shutdownChan: make(chan struct{}),
	}
}

// Start begins the agent runtime.
func (a *AgentRuntime) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("agent already running")
	}
	a.mu.Unlock()

	if err := a.validateConfig(); err != nil {
		return err
	}

	// Start event ingestor
	eventChan, err := a.config.EventIngestor.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start event ingestor: %w", err)
	}
	shutdownChan := make(chan struct{})

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("agent already running")
	}
	a.eventChan = eventChan
	a.shutdownChan = shutdownChan
	a.processingWg.Add(1)
	a.running = true
	a.mu.Unlock()

	go a.processEvents(ctx, shutdownChan)

	a.config.AuditLogger.Log(ctx, ActionAgentStarted, "agent_runtime", map[string]any{
		"timestamp": time.Now().UTC(),
	})

	return nil
}

// Shutdown gracefully stops the agent runtime.
func (a *AgentRuntime) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return nil
	}
	shutdownChan := a.shutdownChan
	a.shutdownChan = nil

	a.config.AuditLogger.Log(ctx, ActionAgentStopping, "agent_runtime", map[string]any{
		"timestamp": time.Now().UTC(),
	})

	// Signal shutdown
	if shutdownChan != nil {
		close(shutdownChan)
	}
	a.mu.Unlock()

	// Stop event ingestor
	if err := a.config.EventIngestor.Stop(ctx); err != nil {
		a.mu.Lock()
		a.running = true
		a.mu.Unlock()
		return fmt.Errorf("failed to stop event ingestor: %w", err)
	}

	// Wait for event processing to complete
	processingDone := make(chan struct{})
	go func() {
		a.processingWg.Wait()
		close(processingDone)
	}()

	select {
	case <-processingDone:
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		return nil
	case <-ctx.Done():
		a.mu.Lock()
		a.running = true
		a.mu.Unlock()
		return fmt.Errorf("shutdown timed out: %w", ctx.Err())
	}
}

// processEvents is the main event processing loop.
func (a *AgentRuntime) processEvents(ctx context.Context, shutdownChan <-chan struct{}) {
	defer a.processingWg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-shutdownChan:
			return
		case event, ok := <-a.eventChan:
			if !ok {
				return
			}
			a.handleEvent(ctx, event)
		}
	}
}

// handleEvent processes a single event.
func (a *AgentRuntime) handleEvent(ctx context.Context, event Event) {
	// Log event receipt
	a.config.AuditLogger.Log(ctx, ActionEventReceived, "event_ingest", map[string]any{
		"event_type": event.Type,
		"topic":      event.Topic,
		"timestamp":  event.Timestamp,
	})

	// Check if playbook should be triggered
	playbook, shouldTrigger := a.shouldTriggerPlaybook(event)
	if !shouldTrigger {
		return
	}

	// Validate action with policy engine
	policyResult := a.config.PolicyEngine.Validate(ctx, Action{
		Type:      ActionPlaybookExecution,
		Playbook:  playbook.Name,
		Event:     event,
		Timestamp: time.Now().UTC(),
	})

	if !policyResult.Approved {
		a.config.AuditLogger.Log(ctx, ActionPolicyRejected, "policy_engine", map[string]any{
			"playbook": playbook.Name,
			"reason":   policyResult.Reason,
		})
		return
	}

	// Execute playbook
	execCtx := ctx
	cancel := func() {}
	if playbook.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, playbook.Timeout)
	}
	defer cancel()

	if err := playbook.Execute(execCtx, event.Payload); err != nil {
		a.config.AuditLogger.Log(ctx, ActionPlaybookFailed, "playbook_execution", map[string]any{
			"playbook": playbook.Name,
			"error":    err.Error(),
		})
		return
	}

	a.config.AuditLogger.Log(ctx, ActionPlaybookCompleted, "playbook_execution", map[string]any{
		"playbook":  playbook.Name,
		"success":   true,
		"timestamp": time.Now().UTC(),
	})
}

// shouldTriggerPlaybook determines if an event should trigger a playbook.
func (a *AgentRuntime) shouldTriggerPlaybook(event Event) (*Playbook, bool) {
	// Simple event-to-playbook mapping
	switch event.Type {
	case "CollectorDegraded":
		if playbook, ok := a.config.PlaybookRegistry.Get("pause_exchange_on_errors"); ok {
			return playbook, true
		}
	case "RiskLimitBreached":
		if payload, ok := event.Payload.(map[string]any); ok {
			if limitType, exists := payload["type"].(string); exists && limitType == "daily_drawdown" {
				if playbook, ok := a.config.PlaybookRegistry.Get("enable_safe_mode_on_drawdown"); ok {
					return playbook, true
				}
			}
		}
	case "CriticalFailure":
		if playbook, ok := a.config.PlaybookRegistry.Get("kill_switch_on_critical"); ok {
			return playbook, true
		}
	}
	return nil, false
}

// IsRunning returns whether the agent is currently running.
func (a *AgentRuntime) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

func (a *AgentRuntime) validateConfig() error {
	switch {
	case a.config.AuditLogger == nil:
		return fmt.Errorf("invalid runtime config: missing audit logger")
	case a.config.EventIngestor == nil:
		return fmt.Errorf("invalid runtime config: missing event ingestor")
	case a.config.PolicyEngine == nil:
		return fmt.Errorf("invalid runtime config: missing policy engine")
	case a.config.PlaybookRegistry == nil:
		return fmt.Errorf("invalid runtime config: missing playbook registry")
	}

	return nil
}
