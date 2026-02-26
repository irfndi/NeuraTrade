package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// HeartbeatConfig holds configuration for the heartbeat system.
type HeartbeatConfig struct {
	// DefaultInterval is the default interval between heartbeat ticks.
	DefaultInterval time.Duration
	// PositionCheckInterval how often to check open positions.
	PositionCheckInterval time.Duration
	// StopLossUpdateInterval how often to update stop-losses.
	StopLossUpdateInterval time.Duration
	// SignalScanInterval how often to scan for new signals.
	SignalScanInterval time.Duration
	// FundingRateCheckInterval how often to check funding rates.
	FundingRateCheckInterval time.Duration
	// ConnectivityCheckInterval how often to verify exchange connectivity.
	ConnectivityCheckInterval time.Duration
	// PolymarketOddsInterval how often to update Polymarket odds.
	PolymarketOddsInterval time.Duration
	// CheckpointInterval how often to checkpoint state.
	CheckpointInterval time.Duration
	// MaxConcurrency bounds concurrent heartbeat tasks.
	MaxConcurrency int
	// DegradedMultiplier expands intervals in degraded mode.
	DegradedMultiplier float64
	// RiskMultiplier shrinks intervals in active-risk/risk-lock mode.
	RiskMultiplier float64
	// Enabled indicates if heartbeat is enabled.
	Enabled bool
}

// DefaultHeartbeatConfig returns a default heartbeat configuration.
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
		MaxConcurrency:            3,
		DegradedMultiplier:        2.0,
		RiskMultiplier:            0.5,
		Enabled:                   true,
	}
}

// HeartbeatTask represents a task that runs periodically.
type HeartbeatTask struct {
	Name             string
	Interval         time.Duration
	LastRun          time.Time
	Handler          func(ctx context.Context) error
	Enabled          bool
	Running          bool
	ErrorCount       int
	LastError        error
	Priority         int
	DisabledReason   string
	BackoffUntil     time.Time
	ConsecutiveError int
}

// TradingHeartbeat manages periodic trading tasks.
type TradingHeartbeat struct {
	mu       sync.RWMutex
	stopOnce sync.Once
	wg       sync.WaitGroup
	config   HeartbeatConfig
	tasks    map[string]*HeartbeatTask
	stopCh   chan struct{}
	running  bool
	logger   *log.Logger
	mode     string
	modeNote string

	// Dependencies.
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
	tradingStateStore interface {
		Checkpoint(ctx context.Context) error
	}
	riskManager interface {
		CheckRiskLimits(ctx context.Context) interface{}
	}
	notificationService *NotificationService
}

var (
	heartbeatRegistryMu sync.RWMutex
	heartbeatRegistry   *TradingHeartbeat
)

const (
	heartbeatModeNormal     = "normal"
	heartbeatModeActiveRisk = "active_risk"
	heartbeatModeDegraded   = "degraded"
	heartbeatModeRiskLock   = "risk_lock"
	heartbeatModeIdle       = "idle"

	prioritySignalScan    = 20
	priorityFundingCheck  = 40
	priorityCheckpoint    = 55
	priorityConnectivity  = 70
	priorityPositionCheck = 90
	priorityStopLoss      = 100

	minHeartbeatInterval = 5 * time.Second
	maxTaskBackoff       = 10 * time.Minute
)

// RegisterHeartbeatRuntime registers the active runtime heartbeat instance for diagnostics endpoints.
func RegisterHeartbeatRuntime(heartbeat *TradingHeartbeat) {
	heartbeatRegistryMu.Lock()
	defer heartbeatRegistryMu.Unlock()
	heartbeatRegistry = heartbeat
}

// CurrentHeartbeatRuntime returns the active runtime heartbeat instance if available.
func CurrentHeartbeatRuntime() *TradingHeartbeat {
	heartbeatRegistryMu.RLock()
	defer heartbeatRegistryMu.RUnlock()
	return heartbeatRegistry
}

// NewTradingHeartbeat creates a new trading heartbeat.
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
	tradingStateStore interface {
		Checkpoint(ctx context.Context) error
	},
	riskManager interface {
		CheckRiskLimits(ctx context.Context) interface{}
	},
	notificationService *NotificationService,
) *TradingHeartbeat {
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 1
	}
	if config.DegradedMultiplier <= 0 {
		config.DegradedMultiplier = 2.0
	}
	if config.RiskMultiplier <= 0 {
		config.RiskMultiplier = 0.5
	}

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
		mode:                heartbeatModeNormal,
	}

	h.registerTasks()
	return h
}

// registerTasks registers all periodic tasks.
func (h *TradingHeartbeat) registerTasks() {
	h.registerTask(
		"position_check",
		"Position Check",
		h.config.PositionCheckInterval,
		h.checkPositions,
		priorityPositionCheck,
		h.positionTracker != nil,
		"position tracker not configured",
	)
	h.registerTask(
		"stop_loss_update",
		"Stop-Loss Update",
		h.config.StopLossUpdateInterval,
		h.updateStopLosses,
		priorityStopLoss,
		h.stopLossService != nil,
		"stop-loss service not configured",
	)
	h.registerTask(
		"signal_scan",
		"Signal Scan",
		h.config.SignalScanInterval,
		h.scanForSignals,
		prioritySignalScan,
		h.signalProcessor != nil,
		"signal processor not configured",
	)
	h.registerTask(
		"funding_check",
		"Funding Rate Check",
		h.config.FundingRateCheckInterval,
		h.checkFundingRates,
		priorityFundingCheck,
		h.fundingCollector != nil,
		"funding collector not configured",
	)
	h.registerTask(
		"connectivity_check",
		"Connectivity Check",
		h.config.ConnectivityCheckInterval,
		h.checkConnectivity,
		priorityConnectivity,
		h.connectivityChecker != nil,
		"connectivity checker not configured",
	)
	h.registerTask(
		"checkpoint",
		"State Checkpoint",
		h.config.CheckpointInterval,
		h.checkpointState,
		priorityCheckpoint,
		h.tradingStateStore != nil,
		"trading state store not configured",
	)
}

func (h *TradingHeartbeat) registerTask(
	key string,
	name string,
	interval time.Duration,
	handler func(ctx context.Context) error,
	priority int,
	enabled bool,
	disabledReason string,
) {
	task := &HeartbeatTask{
		Name:           name,
		Interval:       interval,
		Handler:        handler,
		Priority:       priority,
		Enabled:        enabled,
		DisabledReason: "",
	}
	if !enabled {
		task.DisabledReason = disabledReason
	}
	h.tasks[key] = task
}

// Start begins the heartbeat loop.
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

// Stop halts the heartbeat loop.
func (h *TradingHeartbeat) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}

	h.stopOnce.Do(func() {
		close(h.stopCh)
	})
	h.running = false
	h.mu.Unlock()

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		h.logger.Println("Heartbeat stop timeout waiting for running tasks")
	}
}

// IsRunning returns whether the heartbeat is currently running.
func (h *TradingHeartbeat) IsRunning() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.running
}

// runLoop is the main heartbeat loop.
func (h *TradingHeartbeat) runLoop(ctx context.Context) {
	ticker := time.NewTicker(minHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			h.logger.Println("Heartbeat stopped")
			return
		case <-ctx.Done():
			h.logger.Println("Heartbeat context cancelled")
			return
		case <-ticker.C:
			h.executeTasks(ctx)
		}
	}
}

func (h *TradingHeartbeat) executeTasks(ctx context.Context) {
	mode, modeNote, intervalMultiplier, riskLock := h.determineMode(ctx)
	now := time.Now().UTC()

	type scheduledTask struct {
		name string
		task *HeartbeatTask
	}
	toRun := make([]scheduledTask, 0, len(h.tasks))

	h.mu.Lock()
	h.mode = mode
	h.modeNote = modeNote
	for name, task := range h.tasks {
		if !task.Enabled || task.Running {
			continue
		}
		if !task.BackoffUntil.IsZero() && now.Before(task.BackoffUntil) {
			continue
		}
		if riskLock && task.Priority < priorityConnectivity {
			continue
		}

		effectiveInterval := scaledInterval(task.Interval, intervalMultiplier)
		if task.LastRun.IsZero() || now.Sub(task.LastRun) >= effectiveInterval {
			task.LastRun = now
			task.Running = true
			toRun = append(toRun, scheduledTask{name: name, task: task})
		}
	}
	h.mu.Unlock()

	sort.SliceStable(toRun, func(i, j int) bool {
		return toRun[i].task.Priority > toRun[j].task.Priority
	})

	concurrency := h.config.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)

	for _, scheduled := range toRun {
		sem <- struct{}{}
		h.wg.Add(1)
		go func(name string, task *HeartbeatTask) {
			defer h.wg.Done()
			defer func() { <-sem }()
			h.runTask(ctx, name, task)
		}(scheduled.name, scheduled.task)
	}
}

func scaledInterval(base time.Duration, multiplier float64) time.Duration {
	if base <= 0 {
		base = minHeartbeatInterval
	}
	if multiplier <= 0 {
		multiplier = 1
	}
	interval := time.Duration(float64(base) * multiplier)
	if interval < minHeartbeatInterval {
		return minHeartbeatInterval
	}
	return interval
}

func (h *TradingHeartbeat) determineMode(ctx context.Context) (mode string, note string, multiplier float64, riskLock bool) {
	riskLock, note = h.isRiskLocked(ctx)
	if riskLock {
		return heartbeatModeRiskLock, note, h.config.RiskMultiplier, true
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	allDisabled := true
	hasBackoff := false
	highPriorityRunning := false
	for _, task := range h.tasks {
		if task.Enabled {
			allDisabled = false
		}
		if !task.BackoffUntil.IsZero() && time.Now().UTC().Before(task.BackoffUntil) {
			hasBackoff = true
		}
		if task.Running && task.Priority >= priorityConnectivity {
			highPriorityRunning = true
		}
	}

	if allDisabled {
		return heartbeatModeIdle, "all tasks disabled", 1.5, false
	}
	if hasBackoff {
		return heartbeatModeDegraded, "task backoff active", h.config.DegradedMultiplier, false
	}
	if highPriorityRunning {
		return heartbeatModeActiveRisk, "risk-priority tasks running", h.config.RiskMultiplier, false
	}
	return heartbeatModeNormal, "", 1, false
}

func (h *TradingHeartbeat) isRiskLocked(ctx context.Context) (bool, string) {
	if h.riskManager == nil {
		return false, ""
	}

	raw := h.riskManager.CheckRiskLimits(ctx)
	switch result := raw.(type) {
	case map[string]interface{}:
		if lock, ok := result["risk_lock"].(bool); ok && lock {
			return true, "risk manager lock"
		}
		if allowed, ok := result["trading_allowed"].(bool); ok && !allowed {
			reason := "trading disallowed by risk manager"
			if reasonRaw, ok := result["reason"].(string); ok && strings.TrimSpace(reasonRaw) != "" {
				reason = strings.TrimSpace(reasonRaw)
			}
			return true, reason
		}
	}
	return false, ""
}

// runTask executes a single heartbeat task.
func (h *TradingHeartbeat) runTask(ctx context.Context, name string, task *HeartbeatTask) {
	err := task.Handler(ctx)
	now := time.Now().UTC()

	h.mu.Lock()
	defer h.mu.Unlock()

	task.Running = false
	if err != nil {
		task.ErrorCount++
		task.LastError = err
		task.ConsecutiveError++
		backoff := backoffDuration(task.Interval, task.ConsecutiveError)
		task.BackoffUntil = now.Add(backoff)
		h.logger.Printf(
			"Heartbeat task %s failed: %v (error count=%d, consecutive=%d, backoff=%s)",
			name,
			err,
			task.ErrorCount,
			task.ConsecutiveError,
			backoff,
		)
		return
	}

	task.LastError = nil
	task.ConsecutiveError = 0
	task.BackoffUntil = time.Time{}
}

func backoffDuration(base time.Duration, failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	if base <= 0 {
		base = minHeartbeatInterval
	}
	exp := math.Pow(2, float64(failures-1))
	backoff := time.Duration(float64(base) * exp)
	if backoff > maxTaskBackoff {
		return maxTaskBackoff
	}
	if backoff < minHeartbeatInterval {
		return minHeartbeatInterval
	}
	return backoff
}

// Task handlers.
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

// GetTaskStatus returns the status of all heartbeat tasks.
func (h *TradingHeartbeat) GetTaskStatus() map[string]HeartbeatTaskStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := make(map[string]HeartbeatTaskStatus)
	for name, task := range h.tasks {
		status[name] = HeartbeatTaskStatus{
			Name:             task.Name,
			Interval:         task.Interval.String(),
			LastRun:          task.LastRun,
			Enabled:          task.Enabled,
			Running:          task.Running,
			ErrorCount:       task.ErrorCount,
			LastError:        task.LastError,
			Priority:         task.Priority,
			DisabledReason:   task.DisabledReason,
			BackoffUntil:     task.BackoffUntil,
			ConsecutiveError: task.ConsecutiveError,
			Mode:             h.mode,
			ModeNote:         h.modeNote,
		}
	}
	return status
}

// HeartbeatTaskStatus represents the status of a heartbeat task.
type HeartbeatTaskStatus struct {
	Name             string
	Interval         string
	LastRun          time.Time
	Enabled          bool
	Running          bool
	ErrorCount       int
	LastError        error
	Priority         int
	DisabledReason   string
	BackoffUntil     time.Time
	ConsecutiveError int
	Mode             string
	ModeNote         string
}
