package services

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type DrawdownStatus string

const (
	DrawdownStatusNormal   DrawdownStatus = "normal"
	DrawdownStatusWarning  DrawdownStatus = "warning"
	DrawdownStatusCritical DrawdownStatus = "critical"
	DrawdownStatusHalted   DrawdownStatus = "halted"
)

type MaxDrawdownConfig struct {
	WarningThreshold  decimal.Decimal `json:"warning_threshold"`
	CriticalThreshold decimal.Decimal `json:"critical_threshold"`
	HaltThreshold     decimal.Decimal `json:"halt_threshold"`
	CheckInterval     time.Duration   `json:"check_interval"`
	RecoveryThreshold decimal.Decimal `json:"recovery_threshold"`
	AutoResumeEnabled bool            `json:"auto_resume_enabled"`
}

func DefaultMaxDrawdownConfig() MaxDrawdownConfig {
	return MaxDrawdownConfig{
		WarningThreshold:  decimal.NewFromFloat(0.05),
		CriticalThreshold: decimal.NewFromFloat(0.10),
		HaltThreshold:     decimal.NewFromFloat(0.15),
		CheckInterval:     time.Minute,
		RecoveryThreshold: decimal.NewFromFloat(0.03),
		AutoResumeEnabled: false,
	}
}

type DrawdownState struct {
	ChatID          string          `json:"chat_id"`
	CurrentDrawdown decimal.Decimal `json:"current_drawdown"`
	PeakValue       decimal.Decimal `json:"peak_value"`
	CurrentValue    decimal.Decimal `json:"current_value"`
	MaxDrawdownSeen decimal.Decimal `json:"max_drawdown_seen"`
	Status          DrawdownStatus  `json:"status"`
	TradingHalted   bool            `json:"trading_halted"`
	HaltedAt        *time.Time      `json:"halted_at,omitempty"`
	RecoveredAt     *time.Time      `json:"recovered_at,omitempty"`
	LastChecked     time.Time       `json:"last_checked"`
	WarningCount    int             `json:"warning_count"`
	HaltCount       int             `json:"halt_count"`
}

type MaxDrawdownMetrics struct {
	mu                sync.RWMutex
	TotalChecks       int64            `json:"total_checks"`
	WarningEvents     int64            `json:"warning_events"`
	CriticalEvents    int64            `json:"critical_events"`
	HaltEvents        int64            `json:"halt_events"`
	RecoveryEvents    int64            `json:"recovery_events"`
	MaxDrawdownRecord decimal.Decimal  `json:"max_drawdown_record"`
	EventsByChatID    map[string]int64 `json:"events_by_chat_id"`
}

func (m *MaxDrawdownMetrics) IncrementTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalChecks++
}

func (m *MaxDrawdownMetrics) IncrementWarning() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WarningEvents++
}

func (m *MaxDrawdownMetrics) IncrementCritical() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CriticalEvents++
}

func (m *MaxDrawdownMetrics) IncrementHalt() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.HaltEvents++
}

func (m *MaxDrawdownMetrics) IncrementRecovery() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecoveryEvents++
}

func (m *MaxDrawdownMetrics) UpdateMaxDrawdown(dd decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dd.GreaterThan(m.MaxDrawdownRecord) {
		m.MaxDrawdownRecord = dd
	}
}

func (m *MaxDrawdownMetrics) IncrementByChatID(chatID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.EventsByChatID == nil {
		m.EventsByChatID = make(map[string]int64)
	}
	m.EventsByChatID[chatID]++
}

func (m *MaxDrawdownMetrics) GetMetrics() MaxDrawdownMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	eventsCopy := make(map[string]int64, len(m.EventsByChatID))
	for k, v := range m.EventsByChatID {
		eventsCopy[k] = v
	}

	return MaxDrawdownMetrics{
		TotalChecks:       m.TotalChecks,
		WarningEvents:     m.WarningEvents,
		CriticalEvents:    m.CriticalEvents,
		HaltEvents:        m.HaltEvents,
		RecoveryEvents:    m.RecoveryEvents,
		MaxDrawdownRecord: m.MaxDrawdownRecord,
		EventsByChatID:    eventsCopy,
	}
}

type MaxDrawdownHalt struct {
	config          MaxDrawdownConfig
	db              DBPool
	states          map[string]*DrawdownState
	metrics         MaxDrawdownMetrics
	mu              sync.RWMutex
	notificationSvc *NotificationService
}

// Risk event types
const (
	RiskEventHalt     = "halt"
	RiskEventCritical = "critical_drawdown"
	RiskEventWarning  = "warning_drawdown"
	RiskEventRecovery = "recovery"
)

// Risk event severities
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
)

func NewMaxDrawdownHalt(db DBPool, config MaxDrawdownConfig) *MaxDrawdownHalt {
	return &MaxDrawdownHalt{
		config:  config,
		db:      db,
		states:  make(map[string]*DrawdownState),
		metrics: MaxDrawdownMetrics{EventsByChatID: make(map[string]int64)},
	}
}

func NewMaxDrawdownHaltWithNotification(db DBPool, config MaxDrawdownConfig, notificationSvc *NotificationService) *MaxDrawdownHalt {
	return &MaxDrawdownHalt{
		config:          config,
		db:              db,
		states:          make(map[string]*DrawdownState),
		metrics:         MaxDrawdownMetrics{EventsByChatID: make(map[string]int64)},
		notificationSvc: notificationSvc,
	}
}

func (h *MaxDrawdownHalt) CheckDrawdown(ctx context.Context, chatID string, currentValue decimal.Decimal) (*DrawdownState, error) {
	h.metrics.IncrementTotal()

	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.states[chatID]
	if !exists {
		state = &DrawdownState{
			ChatID:       chatID,
			PeakValue:    currentValue,
			CurrentValue: currentValue,
			Status:       DrawdownStatusNormal,
			LastChecked:  time.Now().UTC(),
		}
		h.states[chatID] = state
	}

	state.CurrentValue = currentValue
	state.LastChecked = time.Now().UTC()

	if currentValue.GreaterThan(state.PeakValue) {
		state.PeakValue = currentValue
	}

	if state.PeakValue.IsZero() {
		state.CurrentDrawdown = decimal.Zero
	} else {
		state.CurrentDrawdown = state.PeakValue.Sub(currentValue).Div(state.PeakValue)
	}

	if state.CurrentDrawdown.GreaterThan(state.MaxDrawdownSeen) {
		state.MaxDrawdownSeen = state.CurrentDrawdown
		h.metrics.UpdateMaxDrawdown(state.CurrentDrawdown)
	}

	h.updateState(state)

	return state, nil
}

func (h *MaxDrawdownHalt) updateState(state *DrawdownState) {
	previousStatus := state.Status

	if state.CurrentDrawdown.GreaterThanOrEqual(h.config.HaltThreshold) {
		state.Status = DrawdownStatusHalted
		if !state.TradingHalted {
			state.TradingHalted = true
			now := time.Now().UTC()
			state.HaltedAt = &now
			state.HaltCount++
			h.metrics.IncrementHalt()
			h.metrics.IncrementByChatID(state.ChatID)
			h.notifyRiskEvent(state, RiskEventHalt, SeverityCritical)
		}
	} else if state.CurrentDrawdown.GreaterThanOrEqual(h.config.CriticalThreshold) {
		state.Status = DrawdownStatusCritical
		if previousStatus != DrawdownStatusCritical {
			h.metrics.IncrementCritical()
			h.notifyRiskEvent(state, RiskEventCritical, SeverityHigh)
		}
	} else if state.CurrentDrawdown.GreaterThanOrEqual(h.config.WarningThreshold) {
		state.Status = DrawdownStatusWarning
		if previousStatus != DrawdownStatusWarning {
			state.WarningCount++
			h.metrics.IncrementWarning()
			h.notifyRiskEvent(state, RiskEventWarning, SeverityMedium)
		}
	} else {
		if state.TradingHalted && state.CurrentDrawdown.LessThanOrEqual(h.config.RecoveryThreshold) {
			if h.config.AutoResumeEnabled {
				state.TradingHalted = false
				state.Status = DrawdownStatusNormal
				now := time.Now().UTC()
				state.RecoveredAt = &now
				h.metrics.IncrementRecovery()
				h.notifyRiskEvent(state, RiskEventRecovery, SeverityLow)
			} else {
				state.Status = DrawdownStatusNormal
			}
		} else {
			state.Status = DrawdownStatusNormal
		}
	}
}

// notifyRiskEvent sends a risk event notification.
// PRECONDITION: Caller must hold h.mu (write lock) before calling this method.
func (h *MaxDrawdownHalt) notifyRiskEvent(state *DrawdownState, eventType, severity string) {
	if h.notificationSvc == nil {
		return
	}

	chatIDInt, err := strconv.ParseInt(state.ChatID, 10, 64)
	if err != nil {
		return
	}

	drawdownPct := state.CurrentDrawdown.Mul(decimal.NewFromInt(100))
	event := RiskEventNotification{
		EventType: eventType,
		Severity:  severity,
		Message:   fmt.Sprintf("Drawdown status changed to %s", state.Status),
		Details: map[string]string{
			"current_drawdown": fmt.Sprintf("%.2f%%", drawdownPct.InexactFloat64()),
			"peak_value":       state.PeakValue.String(),
			"current_value":    state.CurrentValue.String(),
			"status":           string(state.Status),
		},
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.notificationSvc.NotifyRiskEvent(ctx, chatIDInt, event); err != nil {
			if h.notificationSvc.logger != nil {
				h.notificationSvc.logger.Error("Failed to send risk event notification",
					"chat_id", chatIDInt,
					"event_type", eventType,
					"error", err)
			}
		}
	}()
}

func (h *MaxDrawdownHalt) SetNotificationService(svc *NotificationService) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notificationSvc = svc
}

func (h *MaxDrawdownHalt) IsTradingHalted(chatID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	state, exists := h.states[chatID]
	if !exists {
		return false
	}
	return state.TradingHalted
}

func (h *MaxDrawdownHalt) GetState(chatID string) (*DrawdownState, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	state, exists := h.states[chatID]
	if !exists {
		return nil, false
	}
	return state, true
}

func (h *MaxDrawdownHalt) ForceHalt(ctx context.Context, chatID string, _ string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.states[chatID]
	if !exists {
		state = &DrawdownState{
			ChatID:      chatID,
			Status:      DrawdownStatusHalted,
			LastChecked: time.Now().UTC(),
		}
		h.states[chatID] = state
	}

	state.TradingHalted = true
	state.Status = DrawdownStatusHalted
	now := time.Now().UTC()
	state.HaltedAt = &now
	state.HaltCount++
	h.metrics.IncrementHalt()

	return nil
}

func (h *MaxDrawdownHalt) ResumeTrading(ctx context.Context, chatID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.states[chatID]
	if !exists {
		return fmt.Errorf("no state found for chat %s", chatID)
	}

	if !state.TradingHalted {
		return fmt.Errorf("trading is not halted for chat %s", chatID)
	}

	state.TradingHalted = false
	state.Status = DrawdownStatusNormal
	now := time.Now().UTC()
	state.RecoveredAt = &now
	h.metrics.IncrementRecovery()

	return nil
}

func (h *MaxDrawdownHalt) GetMetrics() MaxDrawdownMetrics {
	return h.metrics.GetMetrics()
}

func (h *MaxDrawdownHalt) SetConfig(config MaxDrawdownConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config = config
}

func (h *MaxDrawdownHalt) GetConfig() MaxDrawdownConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config
}

func (h *MaxDrawdownHalt) ShouldAllowTrade(chatID string) bool {
	return !h.IsTradingHalted(chatID)
}

func (h *MaxDrawdownHalt) GetAllHalted() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	halted := make([]string, 0)
	for chatID, state := range h.states {
		if state.TradingHalted {
			halted = append(halted, chatID)
		}
	}
	return halted
}

func (h *MaxDrawdownHalt) GetStatusSummary() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	normal := 0
	warning := 0
	critical := 0
	halted := 0

	for _, state := range h.states {
		switch state.Status {
		case DrawdownStatusNormal:
			normal++
		case DrawdownStatusWarning:
			warning++
		case DrawdownStatusCritical:
			critical++
		case DrawdownStatusHalted:
			halted++
		}
	}

	return map[string]interface{}{
		"total_accounts": len(h.states),
		"normal":         normal,
		"warning":        warning,
		"critical":       critical,
		"halted":         halted,
	}
}

func (h *MaxDrawdownHalt) CalculateDrawdown(peak, current decimal.Decimal) decimal.Decimal {
	if peak.IsZero() {
		return decimal.Zero
	}
	return peak.Sub(current).Div(peak)
}

func (h *MaxDrawdownHalt) ResetPeak(ctx context.Context, chatID string, newValue decimal.Decimal) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.states[chatID]
	if !exists {
		state = &DrawdownState{
			ChatID:      chatID,
			LastChecked: time.Now().UTC(),
		}
		h.states[chatID] = state
	}

	state.PeakValue = newValue
	state.CurrentValue = newValue
	state.CurrentDrawdown = decimal.Zero

	return nil
}
