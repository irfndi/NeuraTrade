package risk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
)

// mockHealthChecker is a controllable health checker for testing.
type mockHealthChecker struct {
	healthy atomic.Bool
}

func newMockHealthChecker(healthy bool) *mockHealthChecker {
	h := &mockHealthChecker{}
	h.healthy.Store(healthy)
	return h
}

func (h *mockHealthChecker) check(_ context.Context) bool {
	return h.healthy.Load()
}

func (h *mockHealthChecker) setHealthy(v bool) {
	h.healthy.Store(v)
}

func TestKillSwitchMonitor_NoEngagementWhenHealthy(t *testing.T) {
	ks := NewKillSwitch()
	checker := newMockHealthChecker(true)
	monitor := NewKillSwitchMonitor(ks, nil, checker.check, KillSwitchMonitorConfig{
		PollInterval:     10 * time.Millisecond,
		FailureThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	// Let it tick a few times
	time.Sleep(50 * time.Millisecond)
	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ks.IsEngaged() {
		t.Fatal("kill switch should not be engaged when healthy")
	}
	if monitor.ConsecutiveFailures() != 0 {
		t.Fatalf("expected 0 consecutive failures, got %d", monitor.ConsecutiveFailures())
	}
}

func TestKillSwitchMonitor_EngagesAfterThreshold(t *testing.T) {
	ks := NewKillSwitch()
	checker := newMockHealthChecker(false)
	monitor := NewKillSwitchMonitor(ks, nil, checker.check, KillSwitchMonitorConfig{
		PollInterval:     10 * time.Millisecond,
		FailureThreshold: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	// Wait for threshold to be reached
	deadline := time.After(2 * time.Second)
	for {
		if monitor.IsEngagedByMonitor() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for kill switch to engage")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ks.IsEngaged() {
		t.Fatal("kill switch should be engaged")
	}
	if !monitor.IsEngagedByMonitor() {
		t.Fatal("monitor should report engaged")
	}
}

func TestKillSwitchMonitor_DisengagesOnRecovery(t *testing.T) {
	ks := NewKillSwitch()
	checker := newMockHealthChecker(false)
	monitor := NewKillSwitchMonitor(ks, nil, checker.check, KillSwitchMonitorConfig{
		PollInterval:     10 * time.Millisecond,
		FailureThreshold: 2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	// Wait for engagement
	deadline := time.After(2 * time.Second)
	for {
		if monitor.IsEngagedByMonitor() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for kill switch to engage")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Now make it healthy
	checker.setHealthy(true)

	// Wait for disengagement
	deadline = time.After(2 * time.Second)
	for {
		if !monitor.IsEngagedByMonitor() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for kill switch to disengage")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ks.IsEngaged() {
		t.Fatal("kill switch should be disengaged after recovery")
	}
}

func TestKillSwitchMonitor_FailureCounterResetsOnHealthy(t *testing.T) {
	ks := NewKillSwitch()
	checker := newMockHealthChecker(false)
	monitor := NewKillSwitchMonitor(ks, nil, checker.check, KillSwitchMonitorConfig{
		PollInterval:     10 * time.Millisecond,
		FailureThreshold: 5,
		RecoveryThreshold: 1,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	// Wait for 2 failures
	time.Sleep(50 * time.Millisecond)
	if monitor.ConsecutiveFailures() < 1 {
		t.Fatal("expected at least 1 failure")
	}

	// Make healthy — counter should reset
	checker.setHealthy(true)
	time.Sleep(30 * time.Millisecond)

	if monitor.ConsecutiveFailures() != 0 {
		t.Fatalf("expected 0 consecutive failures after healthy, got %d", monitor.ConsecutiveFailures())
	}
	if ks.IsEngaged() {
		t.Fatal("kill switch should not be engaged")
	}

	cancel()
	<-errCh
}

func TestKillSwitchMonitor_EmitsEventsOnStateChange(t *testing.T) {
	ks := NewKillSwitch()
	bus := eventbus.New(eventbus.DefaultConfig())
	checker := newMockHealthChecker(false)

	var received []string
	var eventCount atomic.Int32

	// Subscribe to system events
	ctx := context.Background()
	_, err := bus.Subscribe(ctx, "system", func(_ context.Context, event eventbus.Event) error {
		received = append(received, event.Type)
		eventCount.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	monitor := NewKillSwitchMonitor(ks, bus, checker.check, KillSwitchMonitorConfig{
		PollInterval:     10 * time.Millisecond,
		FailureThreshold: 2,
	})

	monCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(monCtx) }()

	// Wait for engagement (should emit ExchangeDisconnected)
	deadline := time.After(2 * time.Second)
	for {
		if monitor.IsEngagedByMonitor() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for kill switch to engage")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Now recover (should emit CollectorRecovered)
	checker.setHealthy(true)
	deadline = time.After(2 * time.Second)
	for {
		if !monitor.IsEngagedByMonitor() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for kill switch to disengage")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Allow event processing
	time.Sleep(50 * time.Millisecond)

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bus.Stop()

	// We expect at least: CollectorDegraded (on first failure) + ExchangeDisconnected (on engage) + CollectorRecovered (on recover)
	if eventCount.Load() < 3 {
		t.Fatalf("expected at least 3 events, got %d (events: %v)", eventCount.Load(), received)
	}

	// Check that we got the expected event types
	hasEvent := func(typ string) bool {
		for _, e := range received {
			if e == typ {
				return true
			}
		}
		return false
	}
	if !hasEvent(ports.EventTypeExchangeDisconnected) {
		t.Errorf("expected ExchangeDisconnected event, got %v", received)
	}
	if !hasEvent(ports.EventTypeCollectorRecovered) {
		t.Errorf("expected CollectorRecovered event, got %v", received)
	}
}

func TestKillSwitchMonitor_DoesNotReEngageDuringHealthyWindow(t *testing.T) {
	ks := NewKillSwitch()
	checker := newMockHealthChecker(false)
	monitor := NewKillSwitchMonitor(ks, nil, checker.check, KillSwitchMonitorConfig{
		PollInterval:     10 * time.Millisecond,
		FailureThreshold: 2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	// Wait for engagement
	deadline := time.After(2 * time.Second)
	for {
		if monitor.IsEngagedByMonitor() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for kill switch to engage")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Even if healthy, the monitor should not re-engage (it's already engaged)
	checker.setHealthy(true)
	time.Sleep(50 * time.Millisecond)

	if !monitor.IsEngagedByMonitor() {
		t.Fatal("kill switch should still be engaged by monitor (disengagement takes time)")
	}

	cancel()
	<-errCh
}

func TestDefaultKillSwitchMonitorConfig(t *testing.T) {
	cfg := DefaultKillSwitchMonitorConfig()
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("expected 5s poll interval, got %v", cfg.PollInterval)
	}
	if cfg.FailureThreshold != 6 {
		t.Errorf("expected failure threshold 6, got %d", cfg.FailureThreshold)
	}
}

func TestNewKillSwitchMonitor_DefaultsApplied(t *testing.T) {
	ks := NewKillSwitch()
	checker := newMockHealthChecker(true)

	// Zero config should get defaults
	monitor := NewKillSwitchMonitor(ks, nil, checker.check, KillSwitchMonitorConfig{})

	if monitor.config.PollInterval != 5*time.Second {
		t.Errorf("expected default 5s poll interval, got %v", monitor.config.PollInterval)
	}
	if monitor.config.FailureThreshold != 6 {
		t.Errorf("expected default failure threshold 6, got %d", monitor.config.FailureThreshold)
	}
}

func TestKillSwitchMonitor_KillSwitchIdempotentEngage(t *testing.T) {
	ks := NewKillSwitch()
	checker := newMockHealthChecker(false)
	monitor := NewKillSwitchMonitor(ks, nil, checker.check, KillSwitchMonitorConfig{
		PollInterval:     10 * time.Millisecond,
		FailureThreshold: 2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	// Wait for engagement
	deadline := time.After(2 * time.Second)
	for {
		if monitor.IsEngagedByMonitor() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for kill switch to engage")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Continue running while unhealthy — should not error (idempotent engage)
	time.Sleep(100 * time.Millisecond)

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ks.IsEngaged() {
		t.Fatal("kill switch should still be engaged")
	}
}
