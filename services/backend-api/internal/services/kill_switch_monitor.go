package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/redis/go-redis/v9"
)

// KillSwitchStatus represents the current state of the kill switch
type KillSwitchStatus string

const (
	KillSwitchStatusActive    KillSwitchStatus = "active"    // Trading is allowed
	KillSwitchStatusTriggered KillSwitchStatus = "triggered" // Kill switch activated, trading halted
	KillSwitchStatusPaused    KillSwitchStatus = "paused"    // Manually paused
)

// KillSwitchTrigger represents what triggered the kill switch
type KillSwitchTrigger string

const (
	KillSwitchTriggerManual      KillSwitchTrigger = "manual"
	KillSwitchTriggerDrawdown    KillSwitchTrigger = "drawdown"
	KillSwitchTriggerLossCap     KillSwitchTrigger = "loss_cap"
	KillSwitchTriggerVolatility  KillSwitchTrigger = "volatility"
	KillSwitchTriggerExchange    KillSwitchTrigger = "exchange_error"
	KillSwitchTriggerConsecutive KillSwitchTrigger = "consecutive_losses"
)

// KillSwitchState represents the current state of the kill switch
type KillSwitchState struct {
	Status        KillSwitchStatus  `json:"status"`
	Trigger       KillSwitchTrigger `json:"trigger,omitempty"`
	TriggeredAt   *time.Time        `json:"triggered_at,omitempty"`
	TriggeredBy   string            `json:"triggered_by,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	AutoRecoverAt *time.Time        `json:"auto_recover_at,omitempty"`
	RecoveredAt   *time.Time        `json:"recovered_at,omitempty"`
	RecoveredBy   string            `json:"recovered_by,omitempty"`
}

// KillSwitchCondition represents a condition that can trigger the kill switch
type KillSwitchCondition struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Trigger     KillSwitchTrigger `json:"trigger"`
	Threshold   float64           `json:"threshold"`
	IsActive    bool              `json:"is_active"`
	LastChecked *time.Time        `json:"last_checked,omitempty"`
}

// KillSwitchMonitor monitors trading conditions and can halt trading when kill switch conditions are met
type KillSwitchMonitor struct {
	mu             sync.RWMutex
	state          *KillSwitchState
	conditions     map[string]*KillSwitchCondition
	redis          *redis.Client
	logger         logging.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	running        bool
	notifier       KillSwitchNotifier
	checkInterval  time.Duration
	cooldownPeriod time.Duration
}

// KillSwitchNotifier defines the interface for kill switch notifications
type KillSwitchNotifier interface {
	NotifyKillSwitchTriggered(ctx context.Context, state *KillSwitchState) error
	NotifyKillSwitchRecovered(ctx context.Context, state *KillSwitchState) error
}

// KillSwitchMonitorConfig holds configuration for the kill switch monitor
type KillSwitchMonitorConfig struct {
	CheckInterval  time.Duration
	CooldownPeriod time.Duration
}

// NewKillSwitchMonitor creates a new kill switch monitor
func NewKillSwitchMonitor(
	redisClient *redis.Client,
	notifier KillSwitchNotifier,
	config *KillSwitchMonitorConfig,
	logger any,
) *KillSwitchMonitor {
	serviceLogger, ok := logger.(logging.Logger)
	if !ok || serviceLogger == nil {
		serviceLogger = logging.NewStandardLogger("info", "production")
	}

	checkInterval := 30 * time.Second
	cooldownPeriod := 5 * time.Minute

	if config != nil {
		if config.CheckInterval > 0 {
			checkInterval = config.CheckInterval
		}
		if config.CooldownPeriod > 0 {
			cooldownPeriod = config.CooldownPeriod
		}
	}

	monitor := &KillSwitchMonitor{
		state: &KillSwitchState{
			Status: KillSwitchStatusActive,
		},
		conditions:     make(map[string]*KillSwitchCondition),
		redis:          redisClient,
		logger:         serviceLogger,
		notifier:       notifier,
		checkInterval:  checkInterval,
		cooldownPeriod: cooldownPeriod,
	}

	// Register default conditions
	monitor.registerDefaultConditions()

	return monitor
}

// registerDefaultConditions registers the default kill switch conditions
func (m *KillSwitchMonitor) registerDefaultConditions() {
	// Maximum drawdown threshold
	m.RegisterCondition(&KillSwitchCondition{
		ID:          "max_drawdown",
		Name:        "Maximum Drawdown",
		Description: "Trigger when portfolio drawdown exceeds threshold",
		Trigger:     KillSwitchTriggerDrawdown,
		Threshold:   0.20, // 20% max drawdown
		IsActive:    true,
	})

	// Daily loss cap
	m.RegisterCondition(&KillSwitchCondition{
		ID:          "daily_loss_cap",
		Name:        "Daily Loss Cap",
		Description: "Trigger when daily losses exceed threshold",
		Trigger:     KillSwitchTriggerLossCap,
		Threshold:   0.05, // 5% daily loss
		IsActive:    true,
	})

	// Consecutive losses
	m.RegisterCondition(&KillSwitchCondition{
		ID:          "consecutive_losses",
		Name:        "Consecutive Losses",
		Description: "Trigger after N consecutive losing trades",
		Trigger:     KillSwitchTriggerConsecutive,
		Threshold:   5, // 5 consecutive losses
		IsActive:    true,
	})

	// High volatility
	m.RegisterCondition(&KillSwitchCondition{
		ID:          "high_volatility",
		Name:        "High Volatility",
		Description: "Trigger when market volatility exceeds threshold",
		Trigger:     KillSwitchTriggerVolatility,
		Threshold:   0.10,  // 10% volatility
		IsActive:    false, // Disabled by default
	})
}

// RegisterCondition registers a kill switch condition
func (m *KillSwitchMonitor) RegisterCondition(condition *KillSwitchCondition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conditions[condition.ID] = condition
}

// Start begins monitoring for kill switch conditions
func (m *KillSwitchMonitor) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("kill switch monitor is already running")
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.running = true

	// Load state from Redis
	if err := m.loadState(); err != nil {
		m.logger.WithError(err).Warn("Failed to load kill switch state from Redis, using default")
	}

	// Start the monitoring loop
	m.wg.Add(1)
	go m.monitorLoop()

	m.logger.Info("Kill switch monitor started")
	observability.AddBreadcrumb(m.ctx, "kill_switch", "Kill switch monitor started", sentry.LevelInfo)
	return nil
}

// Stop gracefully stops the kill switch monitor
func (m *KillSwitchMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.cancel()
	m.wg.Wait()
	m.running = false

	// Save state to Redis
	if err := m.saveState(); err != nil {
		m.logger.WithError(err).Error("Failed to save kill switch state to Redis")
	}

	m.logger.Info("Kill switch monitor stopped")
}

// Trigger activates the kill switch manually or via condition
func (m *KillSwitchMonitor) Trigger(trigger KillSwitchTrigger, triggeredBy, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state.Status == KillSwitchStatusTriggered {
		return fmt.Errorf("kill switch is already triggered")
	}

	now := time.Now()
	m.state.Status = KillSwitchStatusTriggered
	m.state.Trigger = trigger
	m.state.TriggeredAt = &now
	m.state.TriggeredBy = triggeredBy
	m.state.Reason = reason

	// Calculate auto-recovery time
	recoverAt := now.Add(m.cooldownPeriod)
	m.state.AutoRecoverAt = &recoverAt

	m.logger.WithFields(map[string]interface{}{
		"trigger":         trigger,
		"triggered_by":    triggeredBy,
		"reason":          reason,
		"auto_recover_at": recoverAt,
	}).Warn("Kill switch triggered")

	if m.ctx != nil {
		observability.AddBreadcrumb(m.ctx, "kill_switch", fmt.Sprintf("Kill switch triggered: %s", reason), sentry.LevelWarning)
	}

	// Save state
	if err := m.saveState(); err != nil {
		m.logger.WithError(err).Error("Failed to save kill switch state")
	}

	// Copy state for notification to avoid data race
	stateCopy := *m.state

	// Notify
	if m.notifier != nil {
		go func() {
			if err := m.notifier.NotifyKillSwitchTriggered(context.Background(), &stateCopy); err != nil {
				m.logger.WithError(err).Error("Failed to send kill switch notification")
			}
		}()
	}

	return nil
}

// Recover deactivates the kill switch
func (m *KillSwitchMonitor) Recover(recoveredBy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state.Status != KillSwitchStatusTriggered {
		return fmt.Errorf("kill switch is not triggered")
	}

	now := time.Now()
	m.state.RecoveredAt = &now
	m.state.RecoveredBy = recoveredBy
	m.state.Status = KillSwitchStatusActive

	m.logger.WithFields(map[string]interface{}{
		"recovered_by": recoveredBy,
		"trigger":      m.state.Trigger,
	}).Info("Kill switch recovered")

	if m.ctx != nil {
		observability.AddBreadcrumb(m.ctx, "kill_switch", "Kill switch recovered", sentry.LevelInfo)
	}

	// Clear trigger state
	m.state.Trigger = ""
	m.state.TriggeredAt = nil
	m.state.TriggeredBy = ""
	m.state.Reason = ""
	m.state.AutoRecoverAt = nil

	// Save state
	if err := m.saveState(); err != nil {
		m.logger.WithError(err).Error("Failed to save kill switch state")
	}

	// Copy state for notification to avoid data race
	stateCopy := *m.state

	// Notify
	if m.notifier != nil {
		go func() {
			if err := m.notifier.NotifyKillSwitchRecovered(context.Background(), &stateCopy); err != nil {
				m.logger.WithError(err).Error("Failed to send kill switch recovery notification")
			}
		}()
	}

	return nil
}

// Pause temporarily pauses trading without triggering kill switch
func (m *KillSwitchMonitor) Pause(reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state.Status == KillSwitchStatusTriggered {
		return fmt.Errorf("cannot pause when kill switch is triggered")
	}

	m.state.Status = KillSwitchStatusPaused
	m.state.Reason = reason

	m.logger.WithFields(map[string]interface{}{
		"reason": reason,
	}).Info("Kill switch paused")

	return m.saveState()
}

// Resume resumes trading from paused state
func (m *KillSwitchMonitor) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state.Status != KillSwitchStatusPaused {
		return fmt.Errorf("kill switch is not paused")
	}

	m.state.Status = KillSwitchStatusActive
	m.state.Reason = ""

	m.logger.Info("Kill switch resumed")

	return m.saveState()
}

// GetState returns the current kill switch state
func (m *KillSwitchMonitor) GetState() *KillSwitchState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// IsTradingAllowed returns whether trading is currently allowed
func (m *KillSwitchMonitor) IsTradingAllowed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.Status == KillSwitchStatusActive
}

// GetConditions returns all registered conditions
func (m *KillSwitchMonitor) GetConditions() []*KillSwitchCondition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conditions := make([]*KillSwitchCondition, 0, len(m.conditions))
	for _, c := range m.conditions {
		conditions = append(conditions, c)
	}
	return conditions
}

// UpdateCondition updates a specific condition
func (m *KillSwitchMonitor) UpdateCondition(id string, threshold float64, isActive bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	condition, ok := m.conditions[id]
	if !ok {
		return fmt.Errorf("condition not found: %s", id)
	}

	condition.Threshold = threshold
	condition.IsActive = isActive

	return nil
}

// CheckCondition evaluates a specific condition
func (m *KillSwitchMonitor) CheckCondition(id string, currentValue float64) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	condition, ok := m.conditions[id]
	if !ok {
		return false, fmt.Errorf("condition not found: %s", id)
	}

	if !condition.IsActive {
		return false, nil
	}

	return currentValue >= condition.Threshold, nil
}

// monitorLoop runs the periodic monitoring
func (m *KillSwitchMonitor) monitorLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkConditions()
			m.checkAutoRecovery()
		}
	}
}

// checkConditions evaluates all active conditions
func (m *KillSwitchMonitor) checkConditions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Skip if already triggered
	if m.state.Status == KillSwitchStatusTriggered {
		return
	}

	for _, condition := range m.conditions {
		if !condition.IsActive {
			continue
		}

		// Check condition against Redis-stored metrics
		triggered, err := m.evaluateCondition(condition)
		if err != nil {
			m.logger.WithFields(map[string]interface{}{
				"condition_id": condition.ID,
			}).WithError(err).Error("Failed to evaluate condition")
			continue
		}

		if triggered {
			if err := m.Trigger(condition.Trigger, "system", fmt.Sprintf("Condition %s (%s) threshold exceeded", condition.ID, condition.Name)); err != nil {
				m.logger.WithFields(map[string]interface{}{
					"condition_id": condition.ID,
				}).WithError(err).Error("Failed to trigger kill switch")
			}
			return
		}
	}
}

// evaluateCondition evaluates a single condition by checking Redis metrics
func (m *KillSwitchMonitor) evaluateCondition(condition *KillSwitchCondition) (bool, error) {
	if m.redis == nil {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var currentValue float64
	var err error

	switch condition.ID {
	case "max_drawdown":
		currentValue, err = m.getMetricFromRedis(ctx, "portfolio:drawdown")
	case "daily_loss_cap":
		currentValue, err = m.getMetricFromRedis(ctx, "portfolio:daily_loss")
	case "consecutive_losses":
		currentValue, err = m.getMetricFromRedis(ctx, "trading:consecutive_losses")
	case "high_volatility":
		currentValue, err = m.getMetricFromRedis(ctx, "market:volatility")
	default:
		return false, fmt.Errorf("unknown condition: %s", condition.ID)
	}

	if err != nil {
		return false, err
	}

	return currentValue >= condition.Threshold, nil
}

// getMetricFromRedis retrieves a metric value from Redis
func (m *KillSwitchMonitor) getMetricFromRedis(ctx context.Context, key string) (float64, error) {
	val, err := m.redis.Get(ctx, key).Float64()
	if err == redis.Nil {
		return 0, nil // Key doesn't exist, return 0
	}
	return val, err
}

// checkAutoRecovery checks if auto-recovery should be triggered
func (m *KillSwitchMonitor) checkAutoRecovery() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state.Status != KillSwitchStatusTriggered {
		return
	}

	if m.state.AutoRecoverAt == nil {
		return
	}

	if time.Now().After(*m.state.AutoRecoverAt) {
		if err := m.Recover("auto_recovery"); err != nil {
			m.logger.WithError(err).Error("Failed to auto-recover kill switch")
		}
	}
}

// loadState loads the kill switch state from Redis
func (m *KillSwitchMonitor) loadState() error {
	if m.redis == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := m.redis.Get(ctx, "kill_switch:state").Result()
	if err == redis.Nil {
		return nil // No state stored
	}
	if err != nil {
		return err
	}

	// Parse state from Redis (simplified - in production use JSON)
	if data == string(KillSwitchStatusTriggered) {
		m.state.Status = KillSwitchStatusTriggered
	} else if data == string(KillSwitchStatusPaused) {
		m.state.Status = KillSwitchStatusPaused
	}

	return nil
}

// saveState saves the kill switch state to Redis
func (m *KillSwitchMonitor) saveState() error {
	if m.redis == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return m.redis.Set(ctx, "kill_switch:state", string(m.state.Status), 0).Err()
}

// IsRunning returns whether the monitor is running
func (m *KillSwitchMonitor) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}
