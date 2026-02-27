package services

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	eventbusadapter "github.com/irfndi/neuratrade/internal/adapters/eventbus"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
)

func TestHeartbeatActor_ID(t *testing.T) {
	h := NewHeartbeatActor("test-heartbeat", DefaultConfig(), nil, slog.Default())
	if h.ID() != "test-heartbeat" {
		t.Errorf("expected ID 'test-heartbeat', got %s", h.ID())
	}
}

func TestHeartbeatActor_HandleTick(t *testing.T) {
	executed := make(chan string, 1)

	config := DefaultConfig()
	config.Tasks = []TaskConfig{
		{
			Name:     "test-task",
			Interval: time.Millisecond,
			Handler: func(ctx context.Context) error {
				executed <- "test-task"
				return nil
			},
			Enabled: true,
		},
	}

	h := NewHeartbeatActor("test", config, nil, slog.Default())

	// Send tick message
	err := h.Receive(context.Background(), actor.Envelope{
		Message: &TickMessage{Timestamp: time.Now()},
	})
	if err != nil {
		t.Fatalf("failed to handle tick: %v", err)
	}

	// Wait for task execution
	select {
	case name := <-executed:
		if name != "test-task" {
			t.Errorf("expected 'test-task', got %s", name)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for task execution")
	}
}

func TestHeartbeatActor_TaskFailure(t *testing.T) {
	// Set up event bus to capture failure event
	platformBus := eventbus.New(eventbus.DefaultConfig())
	eventBusAdapter := eventbusadapter.NewPlatformEventBusAdapter(platformBus)

	failureReceived := make(chan ports.Event, 1)
	_ = eventBusAdapter.Subscribe(context.Background(), "heartbeat.task_failed", func(ctx context.Context, e ports.Event) error {
		failureReceived <- e
		return nil
	})

	config := DefaultConfig()
	config.Tasks = []TaskConfig{
		{
			Name:     "failing-task",
			Interval: time.Millisecond,
			Handler: func(ctx context.Context) error {
				return errors.New("task failed")
			},
			Enabled:          true,
			BackoffOnFailure: time.Second,
		},
	}

	h := NewHeartbeatActor("test", config, eventBusAdapter, slog.Default())

	// Execute tick
	_ = h.Receive(context.Background(), actor.Envelope{
		Message: &TickMessage{Timestamp: time.Now()},
	})

	// Wait for failure event
	select {
	case e := <-failureReceived:
		if e.EventType() != "heartbeat.task_failed" {
			t.Errorf("expected 'heartbeat.task_failed', got %s", e.EventType())
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for failure event")
	}

	// Check error count
	h.mu.RLock()
	if h.errorCount != 1 {
		t.Errorf("expected error count 1, got %d", h.errorCount)
	}
	h.mu.RUnlock()
}

func TestHeartbeatActor_AddTask(t *testing.T) {
	h := NewHeartbeatActor("test", DefaultConfig(), nil, slog.Default())

	newTask := TaskConfig{
		Name:     "dynamic-task",
		Interval: time.Second,
		Handler:  func(ctx context.Context) error { return nil },
		Enabled:  true,
	}

	err := h.Receive(context.Background(), actor.Envelope{
		Message: &AddTaskMessage{Task: newTask},
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	h.mu.RLock()
	if _, ok := h.tasks["dynamic-task"]; !ok {
		t.Error("task not added")
	}
	h.mu.RUnlock()
}

func TestHeartbeatActor_RemoveTask(t *testing.T) {
	config := DefaultConfig()
	config.Tasks = []TaskConfig{
		{Name: "to-remove", Interval: time.Second, Handler: func(ctx context.Context) error { return nil }, Enabled: true},
	}

	h := NewHeartbeatActor("test", config, nil, slog.Default())

	err := h.Receive(context.Background(), actor.Envelope{
		Message: &RemoveTaskMessage{Name: "to-remove"},
	})
	if err != nil {
		t.Fatalf("failed to remove task: %v", err)
	}

	h.mu.RLock()
	if _, ok := h.tasks["to-remove"]; ok {
		t.Error("task should be removed")
	}
	h.mu.RUnlock()
}

func TestHeartbeatActor_SetMode(t *testing.T) {
	// Set up event bus
	platformBus := eventbus.New(eventbus.DefaultConfig())
	eventBusAdapter := eventbusadapter.NewPlatformEventBusAdapter(platformBus)

	modeReceived := make(chan ports.Event, 1)
	_ = eventBusAdapter.Subscribe(context.Background(), "heartbeat.mode_changed", func(ctx context.Context, e ports.Event) error {
		modeReceived <- e
		return nil
	})

	h := NewHeartbeatActor("test", DefaultConfig(), eventBusAdapter, slog.Default())

	err := h.Receive(context.Background(), actor.Envelope{
		Message: &SetModeMessage{Mode: "degraded"},
	})
	if err != nil {
		t.Fatalf("failed to set mode: %v", err)
	}

	h.mu.RLock()
	if h.mode != "degraded" {
		t.Errorf("expected mode 'degraded', got %s", h.mode)
	}
	h.mu.RUnlock()

	// Check event was published
	select {
	case e := <-modeReceived:
		if e.EventType() != "heartbeat.mode_changed" {
			t.Errorf("expected 'heartbeat.mode_changed', got %s", e.EventType())
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for mode change event")
	}
}

func TestHeartbeatActor_GetStatus(t *testing.T) {
	config := DefaultConfig()
	config.Tasks = []TaskConfig{
		{Name: "task1", Interval: time.Second, Handler: func(ctx context.Context) error { return nil }, Enabled: true},
	}

	h := NewHeartbeatActor("test", config, nil, slog.Default())

	reply := make(chan any, 1)
	err := h.Receive(context.Background(), actor.Envelope{
		Message: &GetStatusMessage{},
		Reply:   reply,
	})
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	select {
	case resp := <-reply:
		status, ok := resp.(StatusResponse)
		if !ok {
			t.Fatal("expected StatusResponse")
		}
		if status.Mode != "normal" {
			t.Errorf("expected mode 'normal', got %s", status.Mode)
		}
		if len(status.Tasks) != 1 {
			t.Errorf("expected 1 task, got %d", len(status.Tasks))
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for status response")
	}
}

func TestHeartbeatActor_EffectiveInterval(t *testing.T) {
	config := DefaultConfig()
	config.DegradedMultiplier = 2.0

	h := NewHeartbeatActor("test", config, nil, slog.Default())

	baseInterval := time.Minute

	// Normal mode
	h.mu.Lock()
	h.mode = "normal"
	h.mu.Unlock()
	if h.effectiveInterval(baseInterval) != baseInterval {
		t.Error("normal mode should not change interval")
	}

	// Degraded mode
	h.mu.Lock()
	h.mode = "degraded"
	h.mu.Unlock()
	if h.effectiveInterval(baseInterval) != 2*baseInterval {
		t.Errorf("degraded mode should double interval, got %v", h.effectiveInterval(baseInterval))
	}

	// Active risk mode
	h.mu.Lock()
	h.mode = "active_risk"
	h.mu.Unlock()
	if h.effectiveInterval(baseInterval) != 30*time.Second {
		t.Errorf("active risk mode should halve interval, got %v", h.effectiveInterval(baseInterval))
	}
}

func TestHeartbeatActor_MaxConcurrency(t *testing.T) {
	var mu sync.Mutex
	running := 0
	maxConcurrent := 0

	config := DefaultConfig()
	config.MaxConcurrency = 2
	config.Tasks = []TaskConfig{
		{Name: "task1", Interval: time.Nanosecond, Handler: func(ctx context.Context) error {
			mu.Lock()
			running++
			if running > maxConcurrent {
				maxConcurrent = running
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			running--
			mu.Unlock()
			return nil
		}, Enabled: true},
		{Name: "task2", Interval: time.Nanosecond, Handler: func(ctx context.Context) error {
			mu.Lock()
			running++
			if running > maxConcurrent {
				maxConcurrent = running
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			running--
			mu.Unlock()
			return nil
		}, Enabled: true},
		{Name: "task3", Interval: time.Nanosecond, Handler: func(ctx context.Context) error {
			mu.Lock()
			running++
			if running > maxConcurrent {
				maxConcurrent = running
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			running--
			mu.Unlock()
			return nil
		}, Enabled: true},
	}

	h := NewHeartbeatActor("test", config, nil, slog.Default())

	_ = h.Receive(context.Background(), actor.Envelope{
		Message: &TickMessage{Timestamp: time.Now()},
	})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	if maxConcurrent > 2 {
		t.Errorf("max concurrency exceeded: %d", maxConcurrent)
	}
	mu.Unlock()
}

func TestHeartbeatActor_UnknownMessage(t *testing.T) {
	h := NewHeartbeatActor("test", DefaultConfig(), nil, slog.Default())

	// Use a simple nil message - Receive handles nil gracefully
	err := h.Receive(context.Background(), actor.Envelope{})
	if err != nil {
		t.Errorf("empty envelope should not error: %v", err)
	}
}

func TestHeartbeatActor_TaskBackoff(t *testing.T) {
	callCount := 0

	config := DefaultConfig()
	config.Tasks = []TaskConfig{
		{
			Name:             "backoff-task",
			Interval:         time.Nanosecond,
			Handler:          func(ctx context.Context) error { callCount++; return errors.New("fail") },
			Enabled:          true,
			BackoffOnFailure: 100 * time.Millisecond,
		},
	}

	h := NewHeartbeatActor("test", config, nil, slog.Default())

	// First tick executes
	_ = h.Receive(context.Background(), actor.Envelope{Message: &TickMessage{Timestamp: time.Now()}})
	time.Sleep(10 * time.Millisecond)

	// Second tick should be in backoff
	_ = h.Receive(context.Background(), actor.Envelope{Message: &TickMessage{Timestamp: time.Now()}})
	time.Sleep(10 * time.Millisecond)

	// Should have only executed once due to backoff
	if callCount != 1 {
		t.Errorf("expected 1 call due to backoff, got %d", callCount)
	}
}

func TestScheduler(t *testing.T) {
	// Create actor system
	actorSystem := actor.NewSystem(actor.DefaultConfig())

	// Create heartbeat actor
	config := DefaultConfig()
	ticksReceived := make(chan time.Time, 3)
	config.Tasks = []TaskConfig{
		{
			Name:     "tick-counter",
			Interval: time.Nanosecond,
			Handler: func(ctx context.Context) error {
				select {
				case ticksReceived <- time.Now():
				default:
				}
				return nil
			},
			Enabled: true,
		},
	}

	h := NewHeartbeatActor("heartbeat", config, nil, slog.Default())
	ref, err := actorSystem.Spawn(h, actor.DefaultConfig())
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}

	// Create scheduler
	scheduler := NewScheduler(ref, 10*time.Millisecond)

	// Start scheduler
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("failed to start scheduler: %v", err)
	}

	// Wait for multiple ticks
	tickCount := 0
	for {
		select {
		case <-ticksReceived:
			tickCount++
			if tickCount >= 3 {
				scheduler.Stop()
				return // Success
			}
		case <-ctx.Done():
			if tickCount < 3 {
				t.Errorf("expected at least 3 ticks, got %d", tickCount)
			}
			return
		}
	}
}

func TestScheduler_DoubleStart(t *testing.T) {
	actorSystem := actor.NewSystem(actor.DefaultConfig())
	h := NewHeartbeatActor("test", DefaultConfig(), nil, slog.Default())
	ref, _ := actorSystem.Spawn(h, actor.DefaultConfig())

	scheduler := NewScheduler(ref, time.Second)

	ctx := context.Background()
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	if err := scheduler.Start(ctx); err == nil {
		t.Error("expected error on double start")
	}

	scheduler.Stop()
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxConcurrency <= 0 {
		t.Error("MaxConcurrency should be positive")
	}
	if config.DegradedMultiplier <= 1.0 {
		t.Error("DegradedMultiplier should be > 1.0")
	}
}
