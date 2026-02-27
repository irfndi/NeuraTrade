// Package services provides business logic services.
// This file provides an actor-based heartbeat system for periodic trading tasks.
// This demonstrates the Actor pattern from the platform package.
package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/ports"
)

// TaskConfig defines a periodic task configuration.
type TaskConfig struct {
	Name             string
	Interval         time.Duration
	Priority         int
	Handler          func(ctx context.Context) error
	Enabled          bool
	BackoffOnFailure time.Duration
}

// Config holds heartbeat service configuration.
type Config struct {
	// MaxConcurrency limits concurrent task execution.
	MaxConcurrency int
	// DegradedMultiplier expands intervals in degraded mode.
	DegradedMultiplier float64
	// Tasks is the list of periodic tasks.
	Tasks []TaskConfig
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxConcurrency:     3,
		DegradedMultiplier: 2.0,
		Tasks:              []TaskConfig{},
	}
}

// HeartbeatActor is an actor that manages periodic trading tasks.
// It processes tick messages and executes scheduled tasks.
type HeartbeatActor struct {
	id       string
	config   Config
	eventBus ports.EventBus
	logger   *slog.Logger

	mu           sync.RWMutex
	tasks        map[string]*taskState
	mode         string
	lastTick     time.Time
	errorCount   int
	successCount int
}

type taskState struct {
	config          TaskConfig
	lastRun         time.Time
	lastError       error
	consecutiveFail int
	backoffUntil    time.Time
}

// NewHeartbeatActor creates a new heartbeat actor.
func NewHeartbeatActor(id string, config Config, eventBus ports.EventBus, logger *slog.Logger) *HeartbeatActor {
	if logger == nil {
		logger = slog.Default()
	}

	tasks := make(map[string]*taskState)
	for _, tc := range config.Tasks {
		tasks[tc.Name] = &taskState{config: tc}
	}

	return &HeartbeatActor{
		id:       id,
		config:   config,
		eventBus: eventBus,
		logger:   logger,
		tasks:    tasks,
		mode:     "normal",
	}
}

// ID returns the actor's unique identifier.
func (h *HeartbeatActor) ID() string {
	return h.id
}

// Receive processes incoming messages.
func (h *HeartbeatActor) Receive(ctx context.Context, env actor.Envelope) error {
	switch msg := env.Message.(type) {
	case *TickMessage:
		return h.handleTick(ctx, msg)
	case *AddTaskMessage:
		return h.handleAddTask(ctx, msg)
	case *RemoveTaskMessage:
		return h.handleRemoveTask(ctx, msg)
	case *SetModeMessage:
		return h.handleSetMode(ctx, msg)
	case *GetStatusMessage:
		return h.handleGetStatus(ctx, env, msg)
	default:
		h.logger.Warn("unknown message type", "type", fmt.Sprintf("%T", msg))
	}
	return nil
}

// TickMessage triggers a heartbeat tick to check and run due tasks.
type TickMessage struct {
	Timestamp time.Time
}

func (m *TickMessage) MessageType() string { return "heartbeat.tick" }

// AddTaskMessage adds a new periodic task.
type AddTaskMessage struct {
	Task TaskConfig
}

func (m *AddTaskMessage) MessageType() string { return "heartbeat.add_task" }

// RemoveTaskMessage removes a task by name.
type RemoveTaskMessage struct {
	Name string
}

func (m *RemoveTaskMessage) MessageType() string { return "heartbeat.remove_task" }

// SetModeMessage sets the operational mode (normal, degraded, active_risk, etc.).
type SetModeMessage struct {
	Mode string
}

func (m *SetModeMessage) MessageType() string { return "heartbeat.set_mode" }

// GetStatusMessage requests the current status.
type GetStatusMessage struct{}

func (m *GetStatusMessage) MessageType() string { return "heartbeat.get_status" }

// StatusResponse contains the current heartbeat status.
type StatusResponse struct {
	Mode         string
	LastTick     time.Time
	ErrorCount   int
	SuccessCount int
	Tasks        []TaskStatus
}

// TaskStatus contains status for a single task.
type TaskStatus struct {
	Name            string
	LastRun         time.Time
	LastError       string
	ConsecutiveFail int
	Enabled         bool
}

func (h *HeartbeatActor) handleTick(ctx context.Context, msg *TickMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.lastTick = msg.Timestamp
	now := msg.Timestamp

	var wg sync.WaitGroup
	sem := make(chan struct{}, h.config.MaxConcurrency)

	for name, state := range h.tasks {
		if !state.config.Enabled {
			continue
		}

		// Calculate effective interval based on mode
		interval := h.effectiveInterval(state.config.Interval)

		// Check if task is due
		if now.Sub(state.lastRun) < interval {
			continue
		}

		// Check backoff
		if !state.backoffUntil.IsZero() && now.Before(state.backoffUntil) {
			continue
		}

		// Execute task with concurrency limit
		wg.Add(1)
		go func(name string, state *taskState) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			h.executeTask(ctx, name, state)
		}(name, state)
	}

	wg.Wait()
	return nil
}

func (h *HeartbeatActor) effectiveInterval(base time.Duration) time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()

	switch h.mode {
	case "degraded":
		return time.Duration(float64(base) * h.config.DegradedMultiplier)
	case "active_risk", "risk_lock":
		return time.Duration(float64(base) * 0.5)
	default:
		return base
	}
}

func (h *HeartbeatActor) executeTask(ctx context.Context, name string, state *taskState) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("task panic", "task", name, "panic", r)
			state.lastError = fmt.Errorf("panic: %v", r)
			state.consecutiveFail++
		}
	}()

	h.logger.Debug("executing task", "task", name)

	if err := state.config.Handler(ctx); err != nil {
		h.logger.Error("task failed", "task", name, "error", err)
		state.lastError = err
		state.consecutiveFail++

		// Apply backoff on failure
		if state.config.BackoffOnFailure > 0 {
			backoff := state.config.BackoffOnFailure * time.Duration(state.consecutiveFail)
			state.backoffUntil = time.Now().Add(backoff)
		}

		h.mu.Lock()
		h.errorCount++
		h.mu.Unlock()

		// Emit failure event
		if h.eventBus != nil {
			_ = h.eventBus.Publish(ctx, &taskFailedEvent{
				BaseEvent: ports.BaseEvent{
					Type:       "heartbeat.task_failed",
					Aggregate:  h.id,
					OccurredAt: time.Now().Unix(),
				},
				TaskName: name,
				Error:    err.Error(),
			})
		}
	} else {
		state.lastError = nil
		state.consecutiveFail = 0
		state.backoffUntil = time.Time{}

		h.mu.Lock()
		h.successCount++
		h.mu.Unlock()
	}

	state.lastRun = time.Now()
}

func (h *HeartbeatActor) handleAddTask(ctx context.Context, msg *AddTaskMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.tasks[msg.Task.Name] = &taskState{config: msg.Task}
	h.logger.Info("task added", "name", msg.Task.Name)
	return nil
}

func (h *HeartbeatActor) handleRemoveTask(ctx context.Context, msg *RemoveTaskMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.tasks[msg.Name]; ok {
		delete(h.tasks, msg.Name)
		h.logger.Info("task removed", "name", msg.Name)
	}
	return nil
}

func (h *HeartbeatActor) handleSetMode(ctx context.Context, msg *SetModeMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.mode = msg.Mode
	h.logger.Info("mode changed", "mode", msg.Mode)

	// Emit mode change event
	if h.eventBus != nil {
		_ = h.eventBus.Publish(ctx, &modeChangedEvent{
			BaseEvent: ports.BaseEvent{
				Type:       "heartbeat.mode_changed",
				Aggregate:  h.id,
				OccurredAt: time.Now().Unix(),
			},
			Mode: msg.Mode,
		})
	}

	return nil
}

func (h *HeartbeatActor) handleGetStatus(ctx context.Context, env actor.Envelope, msg *GetStatusMessage) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := StatusResponse{
		Mode:         h.mode,
		LastTick:     h.lastTick,
		ErrorCount:   h.errorCount,
		SuccessCount: h.successCount,
		Tasks:        make([]TaskStatus, 0, len(h.tasks)),
	}

	for name, state := range h.tasks {
		ts := TaskStatus{
			Name:            name,
			LastRun:         state.lastRun,
			LastError:       "",
			ConsecutiveFail: state.consecutiveFail,
			Enabled:         state.config.Enabled,
		}
		if state.lastError != nil {
			ts.LastError = state.lastError.Error()
		}
		status.Tasks = append(status.Tasks, ts)
	}

	// Send response if reply channel exists
	if env.Reply != nil {
		env.Reply <- status
	}

	return nil
}

// Event types for heartbeat actor.
type taskFailedEvent struct {
	ports.BaseEvent
	TaskName string
	Error    string
}

type modeChangedEvent struct {
	ports.BaseEvent
	Mode string
}

// Scheduler sends periodic tick messages to the heartbeat actor.
type Scheduler struct {
	actor    *actor.Ref
	interval time.Duration
	stopCh   chan struct{}
	running  bool
	mu       sync.Mutex
}

// NewScheduler creates a new scheduler for the heartbeat actor.
func NewScheduler(actorRef *actor.Ref, interval time.Duration) *Scheduler {
	return &Scheduler{
		actor:    actorRef,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins sending periodic tick messages.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("scheduler already running")
	}

	s.running = true

	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				_ = s.actor.Send(ctx, &TickMessage{Timestamp: time.Now()})
			}
		}
	}()

	return nil
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		close(s.stopCh)
		s.running = false
	}
}
