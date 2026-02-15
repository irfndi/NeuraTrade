package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// HeartbeatConfig holds configuration for the heartbeat system
type HeartbeatConfig struct {
	// DefaultInterval is the default interval between heartbeat ticks
	DefaultInterval time.Duration
	// PositionCheckInterval how often to check open positions
	PositionCheckInterval time.Duration
	// StopLossUpdateInterval how often to update stop-losses
	StopLossUpdateInterval time.Duration
	// SignalScanInterval how often to scan for new signals
	SignalScanInterval time.Duration
	// FundingRateCheckInterval how often to check funding rates
	FundingRateCheckInterval time.Duration
	// ConnectivityCheckInterval how often to verify exchange connectivity
	ConnectivityCheckInterval time.Duration
	// PolymarketOddsInterval how often to update Polymarket odds
	PolymarketOddsInterval time.Duration
	// CheckpointInterval how often to checkpoint state
	CheckpointInterval time.Duration
	// Enabled indicates if heartbeat is enabled
	Enabled bool
}

// DefaultHeartbeatConfig returns a default heartbeat configuration
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		DefaultInterval:           30 * time.Minute,
		PositionCheckInterval:     5 * time.Minute,
		StopLossUpdateInterval:    1 * time.Minute,
		SignalScanInterval:        30 * time.Second,
		FundingRateCheckInterval:  60 * time.Minute,
		ConnectivityCheckInterval: 1 * time.Minute,
		PolymarketOddsInterval:    10 * time.Minute,
		CheckpointInterval:        5 * time.Minute,
		Enabled:                   true,
	}
}

// HeartbeatTask represents a task that runs periodically
type HeartbeatTask struct {
	Name       string
	Interval   time.Duration
	LastRun    time.Time
	Handler    func(ctx context.Context) error
	Enabled    bool
	ErrorCount int
	LastError  error
}

// TradingHeartbeat manages periodic trading tasks
type TradingHeartbeat struct {
	mu       sync.RWMutex
	stopOnce sync.Once
	config   HeartbeatConfig
	tasks    map[string]*HeartbeatTask
	stopCh   chan struct{}
	running  bool
	logger   *log.Logger

	// Dependencies
	positionTracker interface {
		SyncPositions(ctx context.Context) error
	}
	stopLossService interface {
		UpdateAllStopLosses(ctx context.Context) error
	}
	signalProcessor interface {
		ScanForSignals(ctx context.Context) error
	}
	fundingCollector interface {
		CheckFundingRates(ctx context.Context) error
	}
	connectivityChecker interface {
		CheckConnectivity(ctx context.Context) error
	}
	tradingStateStore TradingStateStoreInterface
	riskManager       interface {
		CheckRiskLimits(ctx context.Context) interface{}
	}
	notificationService *NotificationService
}

// NewTradingHeartbeat creates a new trading heartbeat
func NewTradingHeartbeat(
	config HeartbeatConfig,
	positionTracker interface {
		SyncPositions(ctx context.Context) error
	},
	stopLossService interface {
		UpdateAllStopLosses(ctx context.Context) error
	},
	signalProcessor interface {
		ScanForSignals(ctx context.Context) error
	},
	fundingCollector interface {
		CheckFundingRates(ctx context.Context) error
	},
	connectivityChecker interface {
		CheckConnectivity(ctx context.Context) error
	},
	tradingStateStore TradingStateStoreInterface,
	riskManager interface {
		CheckRiskLimits(ctx context.Context) interface{}
	},
	notificationService *NotificationService,
) *TradingHeartbeat {
	h := &TradingHeartbeat{
		config:              config,
		tasks:               make(map[string]*HeartbeatTask),
		stopCh:              make(chan struct{}),
		positionTracker:     positionTracker,
		stopLossService:     stopLossService,
		signalProcessor:     signalProcessor,
		fundingCollector:    fundingCollector,
		connectivityChecker: connectivityChecker,
		tradingStateStore:   tradingStateStore,
		riskManager:         riskManager,
		notificationService: notificationService,
		logger:              log.Default(),
	}

	h.registerTasks()
	return h
}

// registerTasks registers all periodic tasks
func (h *TradingHeartbeat) registerTasks() {
	h.tasks["position_check"] = &HeartbeatTask{
		Name:     "Position Check",
		Interval: h.config.PositionCheckInterval,
		Handler:  h.checkPositions,
		Enabled:  true,
	}

	h.tasks["stop_loss_update"] = &HeartbeatTask{
		Name:     "Stop-Loss Update",
		Interval: h.config.StopLossUpdateInterval,
		Handler:  h.updateStopLosses,
		Enabled:  true,
	}

	h.tasks["signal_scan"] = &HeartbeatTask{
		Name:     "Signal Scan",
		Interval: h.config.SignalScanInterval,
		Handler:  h.scanForSignals,
		Enabled:  true,
	}

	h.tasks["funding_check"] = &HeartbeatTask{
		Name:     "Funding Rate Check",
		Interval: h.config.FundingRateCheckInterval,
		Handler:  h.checkFundingRates,
		Enabled:  true,
	}

	h.tasks["connectivity_check"] = &HeartbeatTask{
		Name:     "Connectivity Check",
		Interval: h.config.ConnectivityCheckInterval,
		Handler:  h.checkConnectivity,
		Enabled:  true,
	}

	h.tasks["checkpoint"] = &HeartbeatTask{
		Name:     "State Checkpoint",
		Interval: h.config.CheckpointInterval,
		Handler:  h.checkpointState,
		Enabled:  true,
	}
}

// Start begins the heartbeat loop
func (h *TradingHeartbeat) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return fmt.Errorf("heartbeat already running")
	}
	h.running = true
	h.mu.Unlock()

	go h.runLoop(ctx)
	return nil
}

// Stop halts the heartbeat loop
func (h *TradingHeartbeat) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return
	}

	h.stopOnce.Do(func() {
		close(h.stopCh)
	})
	h.running = false
}

// IsRunning returns whether the heartbeat is currently running
func (h *TradingHeartbeat) IsRunning() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.running
}

// runLoop is the main heartbeat loop
func (h *TradingHeartbeat) runLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			h.logger.Println("Heartbeat stopped")
			return
		case <-ticker.C:
			h.executeTasks(ctx)
		}
	}
}

// executeTasks runs all enabled tasks that are due
func (h *TradingHeartbeat) executeTasks(ctx context.Context) {
	h.mu.RLock()
	now := time.Now()

	for name, task := range h.tasks {
		if !task.Enabled {
			continue
		}

		if now.Sub(task.LastRun) >= task.Interval {
			task := task
			name := name
			h.mu.RUnlock()
			go h.runTask(ctx, name, task)
			h.mu.RLock()
		}
	}
	h.mu.RUnlock()
}

// runTask executes a single heartbeat task
func (h *TradingHeartbeat) runTask(ctx context.Context, name string, task *HeartbeatTask) {
	h.mu.Lock()
	task.LastRun = time.Now()
	h.mu.Unlock()

	err := task.Handler(ctx)

	h.mu.Lock()
	defer h.mu.Unlock()

	if err != nil {
		task.ErrorCount++
		task.LastError = err
		h.logger.Printf("Heartbeat task %s failed: %v (error count: %d)", name, err, task.ErrorCount)
	} else {
		task.ErrorCount = 0
		task.LastError = nil
	}
}

// Task handlers

func (h *TradingHeartbeat) checkPositions(ctx context.Context) error {
	if h.positionTracker == nil {
		return nil
	}
	return h.positionTracker.SyncPositions(ctx)
}

func (h *TradingHeartbeat) updateStopLosses(ctx context.Context) error {
	if h.stopLossService == nil {
		return nil
	}
	return h.stopLossService.UpdateAllStopLosses(ctx)
}

func (h *TradingHeartbeat) scanForSignals(ctx context.Context) error {
	if h.signalProcessor == nil {
		return nil
	}
	return h.signalProcessor.ScanForSignals(ctx)
}

func (h *TradingHeartbeat) checkFundingRates(ctx context.Context) error {
	if h.fundingCollector == nil {
		return nil
	}
	return h.fundingCollector.CheckFundingRates(ctx)
}

func (h *TradingHeartbeat) checkConnectivity(ctx context.Context) error {
	if h.connectivityChecker == nil {
		return nil
	}
	return h.connectivityChecker.CheckConnectivity(ctx)
}

func (h *TradingHeartbeat) checkpointState(ctx context.Context) error {
	if h.tradingStateStore == nil {
		return nil
	}
	return h.tradingStateStore.Checkpoint(ctx)
}

// GetTaskStatus returns the status of all heartbeat tasks
func (h *TradingHeartbeat) GetTaskStatus() map[string]HeartbeatTaskStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := make(map[string]HeartbeatTaskStatus)
	for name, task := range h.tasks {
		status[name] = HeartbeatTaskStatus{
			Name:       task.Name,
			Interval:   task.Interval.String(),
			LastRun:    task.LastRun,
			Enabled:    task.Enabled,
			ErrorCount: task.ErrorCount,
			LastError:  task.LastError,
		}
	}
	return status
}

// HeartbeatTaskStatus represents the status of a heartbeat task
type HeartbeatTaskStatus struct {
	Name       string
	Interval   string
	LastRun    time.Time
	Enabled    bool
	ErrorCount int
	LastError  error
}
