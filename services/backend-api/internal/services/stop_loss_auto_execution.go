package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

// StopLossAutoExecutionConfig holds configuration for automatic stop-loss execution.
type StopLossAutoExecutionConfig struct {
	// CheckInterval is how often to check stop-loss conditions
	CheckInterval time.Duration
	// EnableNotifications enables Telegram notifications
	EnableNotifications bool
	// NotifyChatID is the Telegram chat ID for notifications
	NotifyChatID int64
}

// DefaultStopLossAutoExecutionConfig returns default configuration.
func DefaultStopLossAutoExecutionConfig() StopLossAutoExecutionConfig {
	return StopLossAutoExecutionConfig{
		CheckInterval:       1 * time.Second,
		EnableNotifications: true,
		NotifyChatID:        0,
	}
}

// StopLossAutoExecution monitors prices and automatically executes stop-loss orders.
type StopLossAutoExecution struct {
	config              StopLossAutoExecutionConfig
	stopLossService     *StopLossService
	notificationService *NotificationService
	logger              *zaplogrus.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	statsMu sync.RWMutex
	stats   ExecutionStats
}

// ExecutionStats tracks stop-loss execution statistics.
type ExecutionStats struct {
	TotalChecks       int64           `json:"total_checks"`
	Triggers          int64           `json:"triggers"`
	Successful        int64           `json:"successful"`
	Failed            int64           `json:"failed"`
	LastTriggerTime   *time.Time      `json:"last_trigger_time,omitempty"`
	LastTriggerPrice  decimal.Decimal `json:"last_trigger_price,omitempty"`
	LastTriggerSymbol string          `json:"last_trigger_symbol,omitempty"`
}

// NewStopLossAutoExecution creates a new stop-loss auto-execution service.
func NewStopLossAutoExecution(
	config StopLossAutoExecutionConfig,
	stopLossService *StopLossService,
	notificationService *NotificationService,
	logger *zaplogrus.Logger,
) *StopLossAutoExecution {
	ctx, cancel := context.WithCancel(context.Background())
	return &StopLossAutoExecution{
		config:              config,
		stopLossService:     stopLossService,
		notificationService: notificationService,
		logger:              logger,
		ctx:                 ctx,
		cancel:              cancel,
		stats:               ExecutionStats{},
	}
}

// Start begins the stop-loss monitoring goroutine.
func (s *StopLossAutoExecution) Start() {
	s.wg.Add(1)
	go s.monitorLoop()

	s.logger.Info("Stop-loss auto-execution started",
		"check_interval", s.config.CheckInterval)
}

// Stop stops the stop-loss monitoring.
func (s *StopLossAutoExecution) Stop() {
	s.cancel()
	s.wg.Wait()

	s.logger.Info("Stop-loss auto-execution stopped")
}

// monitorLoop periodically evaluates stop-loss conditions.
func (s *StopLossAutoExecution) monitorLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.evaluateStopLosses()
		}
	}
}

// evaluateStopLosses checks and executes triggered stop-loss orders.
func (s *StopLossAutoExecution) evaluateStopLosses() {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	s.statsMu.Lock()
	s.stats.TotalChecks++
	s.statsMu.Unlock()

	results, err := s.stopLossService.Evaluate(ctx)
	if err != nil {
		s.logger.WithError(err).Error("Failed to evaluate stop-losses")
		return
	}

	for _, result := range results {
		s.statsMu.Lock()
		s.stats.Triggers++

		if result.Success {
			s.stats.Successful++
			now := time.Now().UTC()
			s.stats.LastTriggerTime = &now
			s.stats.LastTriggerPrice = result.ExecutionPrice
			s.stats.LastTriggerSymbol = ""
		} else {
			s.stats.Failed++
		}
		s.statsMu.Unlock()

		s.logger.Info("Stop-loss executed",
			"order_id", result.OrderID,
			"success", result.Success,
			"execution_price", result.ExecutionPrice,
			"realized_pnl", result.RealizedPnL)

		if s.config.EnableNotifications && s.notificationService != nil {
			s.sendNotification(ctx, result)
		}
	}
}

// sendNotification sends a Telegram notification for stop-loss execution.
func (s *StopLossAutoExecution) sendNotification(ctx context.Context, result *StopLossExecutionResult) {
	if s.config.NotifyChatID == 0 {
		return
	}

	message := fmt.Sprintf(
		"🔴 Stop-Loss Executed\n\nOrder: %s\nPrice: $%s\nPnL: $%s\nSlippage: %s%%",
		result.OrderID[:8],
		result.ExecutionPrice.StringFixed(2),
		result.RealizedPnL.StringFixed(2),
		result.SlippagePct.StringFixed(2),
	)

	notification := RiskEventNotification{
		EventType: "stop_loss_executed",
		Severity:  "high",
		Message:   message,
		Details: map[string]string{
			"order_id":        result.OrderID,
			"execution_price": result.ExecutionPrice.String(),
			"realized_pnl":    result.RealizedPnL.String(),
			"slippage_pct":    result.SlippagePct.String(),
		},
	}

	if err := s.notificationService.NotifyRiskEvent(ctx, s.config.NotifyChatID, notification); err != nil {
		s.logger.WithError(err).Error("Failed to send stop-loss notification")
	}
}

// GetStats returns the execution statistics.
func (s *StopLossAutoExecution) GetStats() ExecutionStats {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()
	return s.stats
}

// ManuallyTrigger forces an immediate evaluation of stop-losses.
func (s *StopLossAutoExecution) ManuallyTrigger(ctx context.Context) error {
	s.logger.Info("Manual stop-loss evaluation triggered")
	s.evaluateStopLosses()
	return nil
}
