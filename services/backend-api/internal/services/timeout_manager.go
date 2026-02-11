package services

import (
	"context"
	"sync"
	"time"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
)

// TimeoutConfig defines timeout durations for various types of operations.
type TimeoutConfig struct {
	APICall        time.Duration
	DatabaseQuery  time.Duration
	RedisOperation time.Duration
	ConcurrentOp   time.Duration
	HealthCheck    time.Duration
	Backfill       time.Duration
	SymbolFetch    time.Duration
	MarketData     time.Duration
}

// TimeoutManager handles the creation and management of contexts with timeouts for operations,
// allowing for centralized configuration and monitoring of operation durations.
type TimeoutManager struct {
	config         *TimeoutConfig
	logger         *zaplogrus.Logger
	activeContexts map[string]context.CancelFunc
	mu             sync.RWMutex
	defaultTimeout time.Duration
}

// OperationContext represents a specific operation's context, including its ID, start time, and configured timeout.
type OperationContext struct {
	Ctx         context.Context
	Cancel      context.CancelFunc
	OperationID string
	StartTime   time.Time
	Timeout     time.Duration
}

// NewTimeoutManager creates a new instance of TimeoutManager.
//
// Parameters:
//   - config: The configuration defining timeout durations. If nil, defaults are used.
//   - logger: The logger instance.
//
// Returns:
//   - A pointer to the initialized TimeoutManager.
func NewTimeoutManager(config *TimeoutConfig, logger *zaplogrus.Logger) *TimeoutManager {
	if config == nil {
		config = DefaultTimeoutConfig()
	}

	return &TimeoutManager{
		config:         config,
		logger:         logger,
		activeContexts: make(map[string]context.CancelFunc),
		defaultTimeout: 30 * time.Second,
	}
}

// DefaultTimeoutConfig returns a TimeoutConfig with sensible default values.
func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		APICall:        10 * time.Second,
		DatabaseQuery:  5 * time.Second,
		RedisOperation: 2 * time.Second,
		ConcurrentOp:   15 * time.Second,
		HealthCheck:    3 * time.Second,
		Backfill:       60 * time.Second,
		SymbolFetch:    20 * time.Second,
		MarketData:     8 * time.Second,
	}
}

// CreateOperationContext creates a new context with a timeout suitable for the given operation type.
// It tracks the operation to allow for cancellation and monitoring.
//
// Parameters:
//   - operationType: The type of operation (e.g., "api_call", "database_query").
//   - operationID: A unique identifier for the operation instance.
//
// Returns:
//   - A pointer to the created OperationContext.
func (tm *TimeoutManager) CreateOperationContext(operationType string, operationID string) *OperationContext {
	timeout := tm.getTimeoutForOperation(operationType)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	tm.mu.Lock()
	tm.activeContexts[operationID] = cancel
	tm.mu.Unlock()

	return &OperationContext{
		Ctx:         ctx,
		Cancel:      cancel,
		OperationID: operationID,
		StartTime:   time.Now(),
		Timeout:     timeout,
	}
}

// CreateOperationContextWithParent creates a new context derived from a parent context, with a timeout
// suitable for the given operation type.
//
// Parameters:
//   - parent: The parent context.
//   - operationType: The type of operation.
//   - operationID: A unique identifier for the operation.
//
// Returns:
//   - A pointer to the created OperationContext.
func (tm *TimeoutManager) CreateOperationContextWithParent(parent context.Context, operationType string, operationID string) *OperationContext {
	timeout := tm.getTimeoutForOperation(operationType)
	ctx, cancel := context.WithTimeout(parent, timeout)

	tm.mu.Lock()
	tm.activeContexts[operationID] = cancel
	tm.mu.Unlock()

	return &OperationContext{
		Ctx:         ctx,
		Cancel:      cancel,
		OperationID: operationID,
		StartTime:   time.Now(),
		Timeout:     timeout,
	}
}

// CreateOperationContextWithCustomTimeout creates a new context with a specific timeout duration.
//
// Parameters:
//   - operationID: A unique identifier for the operation.
//   - timeout: The specific duration for the timeout.
//
// Returns:
//   - A pointer to the created OperationContext.
func (tm *TimeoutManager) CreateOperationContextWithCustomTimeout(operationID string, timeout time.Duration) *OperationContext {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	tm.mu.Lock()
	tm.activeContexts[operationID] = cancel
	tm.mu.Unlock()

	return &OperationContext{
		Ctx:         ctx,
		Cancel:      cancel,
		OperationID: operationID,
		StartTime:   time.Now(),
		Timeout:     timeout,
	}
}

// getTimeoutForOperation resolves the timeout duration based on the operation type string.
func (tm *TimeoutManager) getTimeoutForOperation(operationType string) time.Duration {
	switch operationType {
	case "api_call":
		return tm.config.APICall
	case "database_query":
		return tm.config.DatabaseQuery
	case "redis_operation":
		return tm.config.RedisOperation
	case "concurrent_op":
		return tm.config.ConcurrentOp
	case "health_check":
		return tm.config.HealthCheck
	case "backfill":
		return tm.config.Backfill
	case "symbol_fetch":
		return tm.config.SymbolFetch
	case "market_data":
		return tm.config.MarketData
	default:
		return tm.defaultTimeout
	}
}

// CompleteOperation signals that an operation has finished successfully and cleans up its tracking.
//
// Parameters:
//   - operationID: The unique identifier of the completed operation.
func (tm *TimeoutManager) CompleteOperation(operationID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if cancel, exists := tm.activeContexts[operationID]; exists {
		cancel()
		delete(tm.activeContexts, operationID)
	}
}

// CancelOperation manually cancels an active operation and cleans up its tracking.
//
// Parameters:
//   - operationID: The unique identifier of the operation to cancel.
func (tm *TimeoutManager) CancelOperation(operationID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if cancel, exists := tm.activeContexts[operationID]; exists {
		cancel()
		delete(tm.activeContexts, operationID)
		tm.logger.WithField("operation_id", operationID).Info("Operation cancelled")
	}
}

// CancelAllOperations cancels all currently tracked operations. This is useful during shutdown.
func (tm *TimeoutManager) CancelAllOperations() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for operationID, cancel := range tm.activeContexts {
		cancel()
		tm.logger.WithField("operation_id", operationID).Info("Operation cancelled during shutdown")
	}

	tm.activeContexts = make(map[string]context.CancelFunc)
}

// GetActiveOperationCount returns the current number of operations being tracked.
//
// Returns:
//   - The count of active operations.
func (tm *TimeoutManager) GetActiveOperationCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.activeContexts)
}

// GetActiveOperations returns a list of IDs for all currently active operations.
//
// Returns:
//   - A slice of operation IDs.
func (tm *TimeoutManager) GetActiveOperations() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	operations := make([]string, 0, len(tm.activeContexts))
	for operationID := range tm.activeContexts {
		operations = append(operations, operationID)
	}

	return operations
}

// ExecuteWithTimeout wraps a function execution with timeout handling.
// It runs the operation in a goroutine and waits for either completion or timeout.
//
// Parameters:
//   - operationType: The type of operation for timeout lookup.
//   - operationID: A unique identifier for this execution.
//   - operation: The function to execute, accepting a context.
//
// Returns:
//   - The result of the operation, or nil if timed out.
//   - An error if the operation fails or times out.
func (tm *TimeoutManager) ExecuteWithTimeout(
	operationType string,
	operationID string,
	operation func(ctx context.Context) (interface{}, error),
) (interface{}, error) {
	opCtx := tm.CreateOperationContext(operationType, operationID)
	defer tm.CompleteOperation(operationID)

	// Create a channel to receive the result
	resultChan := make(chan struct {
		data interface{}
		err  error
	}, 1)

	// Execute the operation in a goroutine
	go func() {
		data, err := operation(opCtx.Ctx)
		resultChan <- struct {
			data interface{}
			err  error
		}{data: data, err: err}
	}()

	// Wait for either completion or timeout
	select {
	case result := <-resultChan:
		duration := time.Since(opCtx.StartTime)
		tm.logger.WithFields(zaplogrus.Fields{
			"operation_type": operationType,
			"operation_id":   operationID,
			"duration":       duration,
			"success":        result.err == nil,
		}).Debug("Operation completed")
		return result.data, result.err

	case <-opCtx.Ctx.Done():
		duration := time.Since(opCtx.StartTime)
		tm.logger.WithFields(zaplogrus.Fields{
			"operation_type": operationType,
			"operation_id":   operationID,
			"duration":       duration,
			"timeout":        opCtx.Timeout,
		}).Warn("Operation timed out")
		return nil, opCtx.Ctx.Err()
	}
}

// ExecuteWithTimeoutAndFallback executes an operation with a timeout and, if it fails or times out,
// executes a fallback function.
//
// Parameters:
//   - operationType: The type of operation.
//   - operationID: A unique identifier.
//   - operation: The primary function to execute.
//   - fallback: The function to execute if the primary operation fails.
//
// Returns:
//   - The result of the primary or fallback operation.
//   - An error if both fail.
func (tm *TimeoutManager) ExecuteWithTimeoutAndFallback(
	operationType string,
	operationID string,
	operation func(ctx context.Context) (interface{}, error),
	fallback func() (interface{}, error),
) (interface{}, error) {
	result, err := tm.ExecuteWithTimeout(operationType, operationID, operation)
	if err != nil && fallback != nil {
		tm.logger.WithFields(zaplogrus.Fields{
			"operation_type": operationType,
			"operation_id":   operationID,
			"error":          err.Error(),
		}).Info("Executing fallback operation")

		return fallback()
	}

	return result, err
}

// MonitorOperationHealth periodically logs the state of active operations.
// It can warn if the number of active operations exceeds a threshold.
func (tm *TimeoutManager) MonitorOperationHealth() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		activeCount := tm.GetActiveOperationCount()
		tm.logger.WithField("active_operations", activeCount).Debug("Operation health check")

		// Log warning if too many operations are active
		if activeCount > 100 {
			tm.logger.WithField("active_operations", activeCount).Warn("High number of active operations detected")
		}
	}
}

// UpdateTimeoutConfig safely updates the timeout configuration at runtime.
//
// Parameters:
//   - config: The new timeout configuration.
func (tm *TimeoutManager) UpdateTimeoutConfig(config *TimeoutConfig) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.config = config
	tm.logger.Info("Timeout configuration updated")
}

// GetTimeoutConfig retrieves the current timeout configuration.
//
// Returns:
//   - A pointer to the current TimeoutConfig.
func (tm *TimeoutManager) GetTimeoutConfig() *TimeoutConfig {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.config
}

// IsOperationActive checks if a specific operation is currently being tracked.
//
// Parameters:
//   - operationID: The ID of the operation to check.
//
// Returns:
//   - True if the operation is active, false otherwise.
func (tm *TimeoutManager) IsOperationActive(operationID string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	_, exists := tm.activeContexts[operationID]
	return exists
}

// GetOperationStats returns a map of statistics regarding the timeout manager's state.
//
// Returns:
//   - A map containing active operation count, current config, and default timeout.
func (tm *TimeoutManager) GetOperationStats() map[string]interface{} {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return map[string]interface{}{
		"active_operations": len(tm.activeContexts),
		"timeout_config":    tm.config,
		"default_timeout":   tm.defaultTimeout,
	}
}

// Shutdown stops the timeout manager, cancelling all active operations.
func (tm *TimeoutManager) Shutdown() {
	tm.logger.Info("Shutting down timeout manager")
	tm.CancelAllOperations()
	tm.logger.Info("Timeout manager shutdown complete")
}
