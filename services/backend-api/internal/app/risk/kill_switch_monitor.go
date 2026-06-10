package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
)

// DefaultKillSwitchMonitorConfig returns a KillSwitchMonitorConfig with sensible defaults.
func DefaultKillSwitchMonitorConfig() KillSwitchMonitorConfig {
	return KillSwitchMonitorConfig{
		PollInterval:     5 * time.Second,
		FailureThreshold: 6,
		RecoveryThreshold: 10,
	}
}

// KillSwitchMonitorConfig holds configuration for the kill switch monitor.
type KillSwitchMonitorConfig struct {
	// PollInterval is how often to check exchange health. Default: 5s.
	PollInterval time.Duration

	// FailureThreshold is the number of consecutive health check failures
	// before auto-engaging the kill switch. Default: 6 (30s at 5s intervals).
	FailureThreshold int

	// RecoveryThreshold is the number of consecutive healthy checks
	// required before auto-disengaging the kill switch. Default: 3 (15s at 5s intervals).
	// This prevents flapping and ensures the exchange is truly healthy.
	RecoveryThreshold int
}

// ExchangeHealthChecker checks whether the exchange is reachable.
// It returns true if healthy, false otherwise.
type ExchangeHealthChecker func(ctx context.Context) bool

// KillSwitchMonitor watches exchange health and auto-engages/disengages
// the kill switch on degradation/recovery.
type KillSwitchMonitor struct {
	killSwitch *KillSwitchImpl
	eventBus   *eventbus.Bus
	checker    ExchangeHealthChecker
	config     KillSwitchMonitorConfig

	mu               sync.Mutex
	consecutiveFails  int
	consecutiveHealthy int
	engagedByMonitor  bool
}

// NewKillSwitchMonitor creates a new KillSwitchMonitor.
func NewKillSwitchMonitor(
	killSwitch *KillSwitchImpl,
	eventBus *eventbus.Bus,
	checker ExchangeHealthChecker,
	config KillSwitchMonitorConfig,
) *KillSwitchMonitor {
	defaults := DefaultKillSwitchMonitorConfig()
	if config.PollInterval <= 0 {
		config.PollInterval = defaults.PollInterval
	}
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = defaults.FailureThreshold
	}
	if config.RecoveryThreshold <= 0 {
		config.RecoveryThreshold = defaults.RecoveryThreshold
	}
	return &KillSwitchMonitor{
		killSwitch: killSwitch,
		eventBus:   eventBus,
		checker:    checker,
		config:     config,
	}
}

// Run starts the monitoring loop. It blocks until ctx is cancelled.
// Suitable for wrapping in a supervisor.Group.
func (m *KillSwitchMonitor) Run(ctx context.Context) error {
	zaplogrus.Infof("[kill-switch-monitor] starting (poll=%v, threshold=%d)",
		m.config.PollInterval, m.config.FailureThreshold)

	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			zaplogrus.Infof("[kill-switch-monitor] stopping: %v", ctx.Err())
			return nil
		case <-ticker.C:
			m.checkOnce(ctx)
		}
	}
}

// checkOnce performs a single health check and updates state.
func (m *KillSwitchMonitor) checkOnce(ctx context.Context) {
	healthy := m.checker(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	if healthy {
		m.onHealthy(ctx)
	} else {
		m.onUnhealthy(ctx)
	}
}

// onHealthy handles a successful health check.
func (m *KillSwitchMonitor) onHealthy(ctx context.Context) {
	if !m.engagedByMonitor {
		// Not engaged by us — just reset the failure counter.
		m.consecutiveFails = 0
		m.consecutiveHealthy = 0
		return
	}

	// Kill switch is engaged by this monitor — require consecutive healthy
	// checks before disengaging (prevents flapping).
	m.consecutiveFails = 0
	m.consecutiveHealthy++

	if m.consecutiveHealthy < m.config.RecoveryThreshold {
		zaplogrus.Infof("[kill-switch-monitor] healthy check %d/%d during recovery window",
			m.consecutiveHealthy, m.config.RecoveryThreshold)
		return
	}

	m.engagedByMonitor = false
	m.consecutiveHealthy = 0

	reason := "exchange recovered"
	zaplogrus.Infof("[kill-switch-monitor] exchange recovered, disengaging kill switch")

	// Use background context for disengage to ensure it completes even if caller ctx is done.
	disengageCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.killSwitch.Disengage(disengageCtx); err != nil {
		zaplogrus.Infof("[kill-switch-monitor] failed to disengage kill switch: %v", err)
		return
	}

	m.publishEvent(ctx, ports.EventTypeCollectorRecovered, map[string]any{
		"reason": reason,
	})
}

// onUnhealthy handles a failed health check.
func (m *KillSwitchMonitor) onUnhealthy(ctx context.Context) {
	m.consecutiveFails++

	if m.engagedByMonitor {
		// Already engaged — just keep counting.
		return
	}

	zaplogrus.Infof("[kill-switch-monitor] exchange unhealthy (consecutive_failures=%d/%d)",
		m.consecutiveFails, m.config.FailureThreshold)

	m.publishEvent(ctx, ports.EventTypeCollectorDegraded, map[string]any{
		"consecutive_failures": m.consecutiveFails,
		"threshold":            m.config.FailureThreshold,
	})

	if m.consecutiveFails < m.config.FailureThreshold {
		return
	}

	// Threshold reached — engage kill switch.
	m.engagedByMonitor = true
	reason := fmt.Sprintf(
		"exchange unreachable for %v (%d consecutive health check failures)",
		time.Duration(m.consecutiveFails)*m.config.PollInterval,
		m.consecutiveFails,
	)

	zaplogrus.Infof("[kill-switch-monitor] threshold reached, engaging kill switch: %s", reason)

	engageCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Annotate context so the kill switch records the source.
	engageCtx = WithSource(engageCtx, "kill-switch-monitor")

	if err := m.killSwitch.Engage(engageCtx, reason); err != nil {
		zaplogrus.Infof("[kill-switch-monitor] failed to engage kill switch: %v", err)
		m.engagedByMonitor = false
		return
	}

	// Publish event via eventbus.
	m.publishEvent(ctx, ports.EventTypeExchangeDisconnected, map[string]any{
		"reason":               reason,
		"consecutive_failures": m.consecutiveFails,
		"engaged_by":           "kill-switch-monitor",
	})
}

// publishEvent sends an event to the event bus.
func (m *KillSwitchMonitor) publishEvent(ctx context.Context, eventType string, payload map[string]any) {
	if m.eventBus == nil {
		return
	}

	event := eventbus.NewEvent("system", eventType, payload).
		WithSource("kill-switch-monitor")

	if err := m.eventBus.Publish(ctx, event); err != nil {
		zaplogrus.Infof("[kill-switch-monitor] publish event type=%s: %v", eventType, err)
	}
}

// ConsecutiveFailures returns the current consecutive failure count.
// Safe for concurrent use.
func (m *KillSwitchMonitor) ConsecutiveFailures() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.consecutiveFails
}

// IsEngagedByMonitor returns whether the kill switch is currently engaged by this monitor.
// Safe for concurrent use.
func (m *KillSwitchMonitor) IsEngagedByMonitor() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.engagedByMonitor
}
