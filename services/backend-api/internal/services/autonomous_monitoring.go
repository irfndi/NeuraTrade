package services

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// AutonomousMonitoring provides monitoring and alerting for autonomous trading
type AutonomousMonitoring struct {
	mu                  sync.RWMutex
	chatID              string
	startTime           time.Time
	lastQuestUpdate     time.Time
	totalQuests         int
	successfulQuests    int
	failedQuests        int
	totalTrades         int
	profitableTrades    int
	totalPnL            decimal.Decimal
	maxDrawdown         decimal.Decimal
	currentDrawdown     decimal.Decimal
	alertsEnabled       bool
	alertThresholds     MonitoringThresholds
	notificationService *NotificationService
}

// MonitoringThresholds defines alert thresholds
type MonitoringThresholds struct {
	MaxDrawdownPercent   float64 `json:"max_drawdown_percent"`
	MinWinRate           float64 `json:"min_win_rate"`
	MaxConsecutiveLosses int     `json:"max_consecutive_losses"`
	MinProfitPerQuest    float64 `json:"min_profit_per_quest"`
	AlertCooldownMinutes int     `json:"alert_cooldown_minutes"`
}

// DefaultMonitoringThresholds returns default thresholds
func DefaultMonitoringThresholds() MonitoringThresholds {
	return MonitoringThresholds{
		MaxDrawdownPercent:   0.15, // 15% max drawdown
		MinWinRate:           0.50, // 50% minimum win rate
		MaxConsecutiveLosses: 3,    // Max 3 consecutive losses
		MinProfitPerQuest:    0.01, // 1% minimum profit per quest
		AlertCooldownMinutes: 30,   // 30 minutes between alerts
	}
}

// MonitoringSnapshot represents current monitoring state
type MonitoringSnapshot struct {
	ChatID          string          `json:"chat_id"`
	Uptime          time.Duration   `json:"uptime"`
	TotalQuests     int             `json:"total_quests"`
	SuccessRate     float64         `json:"success_rate"`
	TotalTrades     int             `json:"total_trades"`
	WinRate         float64         `json:"win_rate"`
	TotalPnL        decimal.Decimal `json:"total_pnl"`
	CurrentDrawdown decimal.Decimal `json:"current_drawdown"`
	MaxDrawdown     decimal.Decimal `json:"max_drawdown"`
	LastQuestUpdate time.Time       `json:"last_quest_update"`
	HealthStatus    string          `json:"health_status"`
	ActiveAlerts    []string        `json:"active_alerts,omitempty"`
}

// NewAutonomousMonitoring creates a new monitoring instance
func NewAutonomousMonitoring(chatID string, notifService *NotificationService) *AutonomousMonitoring {
	return &AutonomousMonitoring{
		chatID:              chatID,
		startTime:           time.Now(),
		alertsEnabled:       true,
		alertThresholds:     DefaultMonitoringThresholds(),
		notificationService: notifService,
	}
}

// RecordQuestExecution records a quest execution result
func (m *AutonomousMonitoring) RecordQuestExecution(success bool, pnl decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalQuests++
	m.lastQuestUpdate = time.Now()

	if success {
		m.successfulQuests++
	} else {
		m.failedQuests++
	}

	if !pnl.IsZero() {
		m.totalPnL = m.totalPnL.Add(pnl)

		// Update drawdown
		if pnl.LessThan(decimal.Zero) {
			m.currentDrawdown = m.currentDrawdown.Add(pnl.Abs())
			if m.currentDrawdown.GreaterThan(m.maxDrawdown) {
				m.maxDrawdown = m.currentDrawdown
			}
		} else {
			m.currentDrawdown = decimal.Zero
		}
	}

	// Check alerts
	m.checkAlerts()
}

// RecordTrade records a trade execution
func (m *AutonomousMonitoring) RecordTrade(profitable bool, pnl decimal.Decimal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalTrades++
	if profitable {
		m.profitableTrades++
	}

	if !pnl.IsZero() {
		m.totalPnL = m.totalPnL.Add(pnl)
	}

	m.checkAlerts()
}

// GetSnapshot returns current monitoring state
func (m *AutonomousMonitoring) GetSnapshot() MonitoringSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	successRate := 0.0
	if m.totalQuests > 0 {
		successRate = float64(m.successfulQuests) / float64(m.totalQuests)
	}

	winRate := 0.0
	if m.totalTrades > 0 {
		winRate = float64(m.profitableTrades) / float64(m.totalTrades)
	}

	healthStatus := "healthy"
	if m.failedQuests > m.successfulQuests {
		healthStatus = "warning"
	}
	if float64(m.failedQuests)/float64(m.totalQuests) > 0.5 {
		healthStatus = "critical"
	}

	return MonitoringSnapshot{
		ChatID:          m.chatID,
		Uptime:          time.Since(m.startTime),
		TotalQuests:     m.totalQuests,
		SuccessRate:     successRate,
		TotalTrades:     m.totalTrades,
		WinRate:         winRate,
		TotalPnL:        m.totalPnL,
		CurrentDrawdown: m.currentDrawdown,
		MaxDrawdown:     m.maxDrawdown,
		LastQuestUpdate: m.lastQuestUpdate,
		HealthStatus:    healthStatus,
	}
}

// checkAlerts checks if any alert thresholds are breached
func (m *AutonomousMonitoring) checkAlerts() {
	if !m.alertsEnabled {
		return
	}

	snapshot := m.GetSnapshot()
	alerts := []string{}

	// Check drawdown
	drawdownFloat, _ := snapshot.MaxDrawdown.Float64()
	if drawdownFloat > m.alertThresholds.MaxDrawdownPercent {
		alerts = append(alerts, fmt.Sprintf("Max drawdown breached: %.2f%%", drawdownFloat*100))
	}

	// Check win rate
	if snapshot.TotalTrades > 5 && snapshot.WinRate < m.alertThresholds.MinWinRate {
		alerts = append(alerts, fmt.Sprintf("Win rate below threshold: %.1f%%", snapshot.WinRate*100))
	}

	// Check consecutive losses (simplified - would need tracking)
	if m.failedQuests > m.alertThresholds.MaxConsecutiveLosses {
		alerts = append(alerts, fmt.Sprintf("Too many failed quests: %d", m.failedQuests))
	}

	// Send alerts
	for _, alert := range alerts {
		m.sendAlert(alert)
	}
}

// sendAlert sends an alert notification
func (m *AutonomousMonitoring) sendAlert(message string) {
	log.Printf("🚨 ALERT [%s]: %s", m.chatID, message)

	if m.notificationService != nil {
		// TODO: Send actual notification
		// ctx := context.Background()
		// m.notificationService.SendAlert(ctx, m.chatID, message)
	}
}

// EnableAlerts enables alert notifications
func (m *AutonomousMonitoring) EnableAlerts() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alertsEnabled = true
}

// DisableAlerts disables alert notifications
func (m *AutonomousMonitoring) DisableAlerts() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alertsEnabled = false
}

// ResetDrawdown resets drawdown tracking
func (m *AutonomousMonitoring) ResetDrawdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentDrawdown = decimal.Zero
}

// GetPerformanceMetrics returns performance metrics
func (m *AutonomousMonitoring) GetPerformanceMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make(map[string]interface{})
	metrics["uptime_hours"] = time.Since(m.startTime).Hours()
	metrics["total_quests"] = m.totalQuests
	metrics["successful_quests"] = m.successfulQuests
	metrics["failed_quests"] = m.failedQuests
	metrics["success_rate"] = float64(m.successfulQuests) / float64(m.totalQuests)
	metrics["total_trades"] = m.totalTrades
	metrics["profitable_trades"] = m.profitableTrades
	metrics["win_rate"] = float64(m.profitableTrades) / float64(m.totalTrades)
	metrics["total_pnl"] = m.totalPnL.String()
	metrics["max_drawdown"] = m.maxDrawdown.String()
	metrics["current_drawdown"] = m.currentDrawdown.String()

	return metrics
}

// AutonomousMonitorManager manages monitoring for multiple users
type AutonomousMonitorManager struct {
	mu           sync.RWMutex
	monitors     map[string]*AutonomousMonitoring
	notifService *NotificationService
}

// NewAutonomousMonitorManager creates a new monitor manager
func NewAutonomousMonitorManager(notifService *NotificationService) *AutonomousMonitorManager {
	return &AutonomousMonitorManager{
		monitors:     make(map[string]*AutonomousMonitoring),
		notifService: notifService,
	}
}

// GetOrCreateMonitor gets or creates a monitor for a chat ID
func (m *AutonomousMonitorManager) GetOrCreateMonitor(chatID string) *AutonomousMonitoring {
	m.mu.Lock()
	defer m.mu.Unlock()

	if monitor, ok := m.monitors[chatID]; ok {
		return monitor
	}

	monitor := NewAutonomousMonitoring(chatID, m.notifService)
	m.monitors[chatID] = monitor
	return monitor
}

// RecordQuestExecution records quest execution for a chat ID
func (m *AutonomousMonitorManager) RecordQuestExecution(chatID string, success bool, pnl decimal.Decimal) {
	monitor := m.GetOrCreateMonitor(chatID)
	monitor.RecordQuestExecution(success, pnl)
}

// GetSnapshot gets monitoring snapshot for a chat ID
func (m *AutonomousMonitorManager) GetSnapshot(chatID string) MonitoringSnapshot {
	monitor := m.GetOrCreateMonitor(chatID)
	return monitor.GetSnapshot()
}

// GetAllSnapshots gets snapshots for all monitored chat IDs
func (m *AutonomousMonitorManager) GetAllSnapshots() map[string]MonitoringSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshots := make(map[string]MonitoringSnapshot)
	for chatID, monitor := range m.monitors {
		snapshots[chatID] = monitor.GetSnapshot()
	}
	return snapshots
}
