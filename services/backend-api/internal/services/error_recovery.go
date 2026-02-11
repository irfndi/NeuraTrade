package services

import (
	"context"
	"sync"
	"time"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
)

// Note: CircuitBreaker types are defined in circuit_breaker.go

// ErrorRecoveryManager manages error recovery for concurrent operations.
type ErrorRecoveryManager struct {
	logger          *zaplogrus.Logger
	circuitBreakers map[string]*CircuitBreaker
	retryPolicies   map[string]*RetryPolicy
	mu              sync.RWMutex

	// Graceful degradation settings
	degradationMode bool
	fallbackEnabled bool
}

// RetryPolicy defines retry behavior for failed operations.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int
	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration
	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration
	// BackoffFactor is the multiplier for exponential backoff.
	BackoffFactor float64
	// JitterEnabled adds randomness to the delay.
	JitterEnabled bool
}

// OperationResult represents the result of an operation with error recovery.
type OperationResult struct {
	// Success indicates if the operation succeeded.
	Success bool
	// Data is the result data.
	Data interface{}
	// Error is the error if failed.
	Error error
	// Attempts is the number of attempts made.
	Attempts int
	// Duration is the total operation duration.
	Duration time.Duration
	// Recovered indicates if the operation succeeded after a retry.
	Recovered bool
	// FallbackUsed indicates if a fallback function was used.
	FallbackUsed bool
}

// NewErrorRecoveryManager creates a new error recovery manager.
//
// Parameters:
//
//	logger: Logger instance.
//
// Returns:
//
//	*ErrorRecoveryManager: Initialized manager.
func NewErrorRecoveryManager(logger *zaplogrus.Logger) *ErrorRecoveryManager {
	return &ErrorRecoveryManager{
		logger:          logger,
		circuitBreakers: make(map[string]*CircuitBreaker),
		retryPolicies:   make(map[string]*RetryPolicy),
		fallbackEnabled: true,
	}
}

// Note: NewCircuitBreaker is defined in circuit_breaker.go

// Note: CircuitBreaker methods are defined in circuit_breaker.go

// RegisterCircuitBreaker registers a circuit breaker for a specific operation.
//
// Parameters:
//
//	name: Operation name.
//	maxFailures: Failure threshold.
//	timeout: Open state timeout.
func (erm *ErrorRecoveryManager) RegisterCircuitBreaker(name string, maxFailures int64, timeout time.Duration) {
	erm.mu.Lock()
	defer erm.mu.Unlock()

	config := CircuitBreakerConfig{
		FailureThreshold: int(maxFailures),
		SuccessThreshold: 2,
		Timeout:          timeout,
		MaxRequests:      5,
		ResetTimeout:     120 * time.Second,
	}
	erm.circuitBreakers[name] = NewCircuitBreaker(name, config, erm.logger)
}

// RegisterRetryPolicy registers a retry policy for a specific operation.
//
// Parameters:
//
//	name: Operation name.
//	policy: Retry policy configuration.
func (erm *ErrorRecoveryManager) RegisterRetryPolicy(name string, policy *RetryPolicy) {
	erm.mu.Lock()
	defer erm.mu.Unlock()

	erm.retryPolicies[name] = policy
}

// ExecuteWithRecovery executes an operation with full error recovery (circuit breaker, retry, fallback).
//
// Parameters:
//
//	ctx: Context.
//	operationName: Name of the operation.
//	operation: The function to execute.
//	fallback: Optional fallback function.
//
// Returns:
//
//	*OperationResult: Result of the execution.
func (erm *ErrorRecoveryManager) ExecuteWithRecovery(
	ctx context.Context,
	operationName string,
	operation func() (interface{}, error),
	fallback func() (interface{}, error),
) *OperationResult {
	start := time.Now()
	result := &OperationResult{
		Attempts: 0,
	}

	// Get circuit breaker and retry policy
	erm.mu.RLock()
	cb := erm.circuitBreakers[operationName]
	retryPolicy := erm.retryPolicies[operationName]
	erm.mu.RUnlock()

	// Execute with circuit breaker if available
	if cb != nil {
		err := cb.Execute(ctx, func(ctx context.Context) error {
			data, execErr := operation()
			if execErr == nil {
				result.Success = true
				result.Data = data
				result.Attempts = 1
				result.Duration = time.Since(start)
			}
			return execErr
		})
		if err == nil {
			return result
		}

		// Circuit breaker is open, try fallback
		if cb.GetState() == Open && fallback != nil && erm.fallbackEnabled {
			data, err := fallback()
			if err == nil {
				result.Success = true
				result.Data = data
				result.FallbackUsed = true
				result.Attempts = 1
				result.Duration = time.Since(start)
				return result
			}
		}
	}

	// Execute with retry policy if available
	if retryPolicy != nil {
		return erm.executeWithRetry(ctx, operation, fallback, retryPolicy, start)
	}

	// Simple execution without recovery
	data, err := operation()
	result.Attempts = 1
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err
		// Try fallback
		if fallback != nil && erm.fallbackEnabled {
			data, err = fallback()
			if err == nil {
				result.Success = true
				result.Data = data
				result.FallbackUsed = true
			}
		}
	} else {
		result.Success = true
		result.Data = data
	}

	return result
}

// executeWithRetry executes an operation with retry logic
func (erm *ErrorRecoveryManager) executeWithRetry(
	ctx context.Context,
	operation func() (interface{}, error),
	fallback func() (interface{}, error),
	policy *RetryPolicy,
	start time.Time,
) *OperationResult {
	result := &OperationResult{}
	delay := policy.InitialDelay

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		result.Attempts = attempt + 1

		// Check context cancellation
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			result.Duration = time.Since(start)
			return result
		default:
		}

		// Execute operation
		data, err := operation()
		if err == nil {
			result.Success = true
			result.Data = data
			result.Duration = time.Since(start)
			if attempt > 0 {
				result.Recovered = true
			}
			return result
		}

		result.Error = err

		// Don't retry on last attempt
		if attempt == policy.MaxRetries {
			break
		}

		// Wait before retry
		time.Sleep(erm.calculateDelay(delay, policy))
		delay = time.Duration(float64(delay) * policy.BackoffFactor)
		if delay > policy.MaxDelay {
			delay = policy.MaxDelay
		}
	}

	// All retries failed, try fallback
	if fallback != nil && erm.fallbackEnabled {
		data, err := fallback()
		if err == nil {
			result.Success = true
			result.Data = data
			result.FallbackUsed = true
		}
	}

	result.Duration = time.Since(start)
	return result
}

// calculateDelay calculates the delay with optional jitter
func (erm *ErrorRecoveryManager) calculateDelay(baseDelay time.Duration, policy *RetryPolicy) time.Duration {
	if !policy.JitterEnabled {
		return baseDelay
	}

	// Add up to 25% jitter
	jitter := time.Duration(float64(baseDelay) * 0.25 * (0.5 - float64(time.Now().UnixNano()%1000)/1000.0))
	return baseDelay + jitter
}

// EnableDegradationMode enables graceful degradation mode.
func (erm *ErrorRecoveryManager) EnableDegradationMode() {
	erm.mu.Lock()
	defer erm.mu.Unlock()
	erm.degradationMode = true
	erm.logger.Warn("Error recovery manager entered degradation mode")
}

// DisableDegradationMode disables graceful degradation mode.
func (erm *ErrorRecoveryManager) DisableDegradationMode() {
	erm.mu.Lock()
	defer erm.mu.Unlock()
	erm.degradationMode = false
	erm.logger.Info("Error recovery manager exited degradation mode")
}

// IsInDegradationMode returns whether the system is in degradation mode.
//
// Returns:
//
//	bool: True if in degradation mode.
func (erm *ErrorRecoveryManager) IsInDegradationMode() bool {
	erm.mu.RLock()
	defer erm.mu.RUnlock()
	return erm.degradationMode
}

// GetCircuitBreakerStatus returns the status of all circuit breakers.
//
// Returns:
//
//	map[string]interface{}: Status map.
func (erm *ErrorRecoveryManager) GetCircuitBreakerStatus() map[string]interface{} {
	erm.mu.RLock()
	defer erm.mu.RUnlock()

	status := make(map[string]interface{})
	for name, cb := range erm.circuitBreakers {
		status[name] = map[string]interface{}{
			"state":         cb.GetState(),
			"failure_count": cb.GetStats().FailedRequests,
			"last_failure":  cb.lastFailureTime,
		}
	}

	return status
}

// ExecuteWithRetry executes an operation with retry logic only (no circuit breaker).
//
// Parameters:
//
//	ctx: Context.
//	operationName: Operation name.
//	operation: Function to execute.
//
// Returns:
//
//	error: Error if all retries fail.
func (erm *ErrorRecoveryManager) ExecuteWithRetry(
	ctx context.Context,
	operationName string,
	operation func() error,
) error {
	start := time.Now()

	// Get retry policy
	erm.mu.RLock()
	retryPolicy := erm.retryPolicies[operationName]
	erm.mu.RUnlock()

	// Use default policy if none found
	if retryPolicy == nil {
		retryPolicy = &RetryPolicy{
			MaxRetries:    3,
			InitialDelay:  100 * time.Millisecond,
			MaxDelay:      5 * time.Second,
			BackoffFactor: 2.0,
			JitterEnabled: true,
		}
	}

	delay := retryPolicy.InitialDelay
	var lastErr error

	for attempt := 0; attempt <= retryPolicy.MaxRetries; attempt++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Execute operation
		err := operation()
		if err == nil {
			if attempt > 0 {
				erm.logger.WithFields(zaplogrus.Fields{
					"operation": operationName,
					"attempts":  attempt + 1,
					"duration":  time.Since(start),
				}).Info("Operation recovered after retry")
			}
			return nil
		}

		lastErr = err

		// Don't retry on last attempt
		if attempt == retryPolicy.MaxRetries {
			break
		}

		// Log retry attempt
		erm.logger.WithFields(zaplogrus.Fields{
			"operation": operationName,
			"attempt":   attempt + 1,
			"error":     err.Error(),
			"delay":     delay,
		}).Warn("Operation failed, retrying")

		// Wait before retry
		time.Sleep(erm.calculateDelay(delay, retryPolicy))
		delay = time.Duration(float64(delay) * retryPolicy.BackoffFactor)
		if delay > retryPolicy.MaxDelay {
			delay = retryPolicy.MaxDelay
		}
	}

	// All retries failed
	erm.logger.WithFields(zaplogrus.Fields{
		"operation": operationName,
		"attempts":  retryPolicy.MaxRetries + 1,
		"duration":  time.Since(start),
		"error":     lastErr.Error(),
	}).Error("Operation failed after all retries")

	return lastErr
}

// DefaultRetryPolicies returns default retry policies for common operations.
//
// Returns:
//
//	map[string]*RetryPolicy: Map of default policies.
func DefaultRetryPolicies() map[string]*RetryPolicy {
	return map[string]*RetryPolicy{
		"api_call": {
			MaxRetries:    3,
			InitialDelay:  100 * time.Millisecond,
			MaxDelay:      5 * time.Second,
			BackoffFactor: 2.0,
			JitterEnabled: true,
		},
		"database_operation": {
			MaxRetries:    5,
			InitialDelay:  50 * time.Millisecond,
			MaxDelay:      2 * time.Second,
			BackoffFactor: 1.5,
			JitterEnabled: true,
		},
		"redis_operation": {
			MaxRetries:    3,
			InitialDelay:  25 * time.Millisecond,
			MaxDelay:      1 * time.Second,
			BackoffFactor: 2.0,
			JitterEnabled: false,
		},
		"concurrent_operation": {
			MaxRetries:    2,
			InitialDelay:  200 * time.Millisecond,
			MaxDelay:      3 * time.Second,
			BackoffFactor: 2.5,
			JitterEnabled: true,
		},
	}
}
