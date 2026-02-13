package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
)

// TestErrorRecoveryManager_NewErrorRecoveryManager tests error recovery manager creation
func TestErrorRecoveryManager_NewErrorRecoveryManager(t *testing.T) {
	logger := zaplogrus.New()

	erm := NewErrorRecoveryManager(logger)

	assert.NotNil(t, erm)
	assert.Equal(t, logger, erm.logger)
	assert.NotNil(t, erm.circuitBreakers)
	assert.NotNil(t, erm.retryPolicies)
	assert.False(t, erm.degradationMode)
	assert.True(t, erm.fallbackEnabled)
}

// TestErrorRecoveryManager_RegisterCircuitBreaker tests circuit breaker registration
func TestErrorRecoveryManager_RegisterCircuitBreaker(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Register a circuit breaker
	erm.RegisterCircuitBreaker("test_operation", 3, 5*time.Second)

	// Verify it was registered
	erm.mu.RLock()
	cb, exists := erm.circuitBreakers["test_operation"]
	erm.mu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, cb)
}

// TestErrorRecoveryManager_RegisterRetryPolicy tests retry policy registration
func TestErrorRecoveryManager_RegisterRetryPolicy(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	policy := &RetryPolicy{
		MaxRetries:    3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
		JitterEnabled: true,
	}

	// Register a retry policy
	erm.RegisterRetryPolicy("test_operation", policy)

	// Verify it was registered
	erm.mu.RLock()
	retrievedPolicy, exists := erm.retryPolicies["test_operation"]
	erm.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, policy, retrievedPolicy)
}

// TestErrorRecoveryManager_ExecuteWithRecovery_Success tests successful operation execution
func TestErrorRecoveryManager_ExecuteWithRecovery_Success(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	operation := func() (interface{}, error) {
		return "success", nil
	}

	result := erm.ExecuteWithRecovery(context.Background(), "test_operation", operation, nil)

	assert.True(t, result.Success)
	assert.Equal(t, "success", result.Data)
	assert.Nil(t, result.Error)
	assert.Equal(t, 1, result.Attempts)
	assert.False(t, result.Recovered)
	assert.False(t, result.FallbackUsed)
}

// TestErrorRecoveryManager_ExecuteWithRecovery_Failure tests failed operation execution
func TestErrorRecoveryManager_ExecuteWithRecovery_Failure(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	operation := func() (interface{}, error) {
		return nil, errors.New("operation failed")
	}

	result := erm.ExecuteWithRecovery(context.Background(), "test_operation", operation, nil)

	assert.False(t, result.Success)
	assert.Nil(t, result.Data)
	assert.Error(t, result.Error)
	assert.Equal(t, 1, result.Attempts)
	assert.False(t, result.Recovered)
	assert.False(t, result.FallbackUsed)
}

// TestErrorRecoveryManager_ExecuteWithRecovery_Fallback tests fallback execution
func TestErrorRecoveryManager_ExecuteWithRecovery_Fallback(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	operation := func() (interface{}, error) {
		return nil, errors.New("operation failed")
	}

	fallback := func() (interface{}, error) {
		return "fallback_success", nil
	}

	result := erm.ExecuteWithRecovery(context.Background(), "test_operation", operation, fallback)

	assert.True(t, result.Success)
	assert.Equal(t, "fallback_success", result.Data)
	assert.True(t, result.FallbackUsed)
}

// TestErrorRecoveryManager_ExecuteWithRetry tests retry logic
func TestErrorRecoveryManager_ExecuteWithRetry(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Register a retry policy
	policy := &RetryPolicy{
		MaxRetries:    2,
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		JitterEnabled: false,
	}
	erm.RegisterRetryPolicy("test_operation", policy)

	attempts := 0
	operation := func() (interface{}, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("temporary failure")
		}
		return "success_after_retry", nil
	}

	result := erm.ExecuteWithRecovery(context.Background(), "test_operation", operation, nil)

	assert.True(t, result.Success)
	assert.Equal(t, "success_after_retry", result.Data)
	assert.True(t, result.Recovered)
	assert.Equal(t, 3, result.Attempts)
}

// TestErrorRecoveryManager_ExecuteWithRetry_ContextCancellation tests context cancellation
func TestErrorRecoveryManager_ExecuteWithRetry_ContextCancellation(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Register a retry policy
	policy := &RetryPolicy{
		MaxRetries:    5,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
		JitterEnabled: false,
	}
	erm.RegisterRetryPolicy("test_operation", policy)

	ctx, cancel := context.WithCancel(context.Background())

	operation := func() (interface{}, error) {
		cancel() // Cancel context during first attempt
		return nil, errors.New("operation failed")
	}

	result := erm.ExecuteWithRecovery(ctx, "test_operation", operation, nil)

	assert.False(t, result.Success)
	assert.Equal(t, context.Canceled, result.Error)
}

// TestErrorRecoveryManager_CalculateDelay tests delay calculation
func TestErrorRecoveryManager_CalculateDelay(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	policy := &RetryPolicy{
		MaxRetries:    3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
		JitterEnabled: false,
	}

	delay := erm.calculateDelay(100*time.Millisecond, policy)
	assert.Equal(t, 100*time.Millisecond, delay)

	// Test with jitter enabled
	policy.JitterEnabled = true
	delay = erm.calculateDelay(100*time.Millisecond, policy)
	assert.True(t, delay >= 75*time.Millisecond && delay <= 125*time.Millisecond)
}

// TestErrorRecoveryManager_DegradationMode tests degradation mode
func TestErrorRecoveryManager_DegradationMode(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Initially not in degradation mode
	assert.False(t, erm.IsInDegradationMode())

	// Enable degradation mode
	erm.EnableDegradationMode()
	assert.True(t, erm.IsInDegradationMode())

	// Disable degradation mode
	erm.DisableDegradationMode()
	assert.False(t, erm.IsInDegradationMode())
}

// TestErrorRecoveryManager_GetCircuitBreakerStatus tests circuit breaker status retrieval
func TestErrorRecoveryManager_GetCircuitBreakerStatus(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Initially empty status
	status := erm.GetCircuitBreakerStatus()
	assert.Empty(t, status)

	// Register a circuit breaker
	erm.RegisterCircuitBreaker("test_operation", 3, 5*time.Second)

	// Check status
	status = erm.GetCircuitBreakerStatus()
	assert.NotEmpty(t, status)
	assert.Contains(t, status, "test_operation")
}

// TestErrorRecoveryManager_ExecuteWithRetry_ErrorOnly tests error-only retry execution
func TestErrorRecoveryManager_ExecuteWithRetry_ErrorOnly(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	ctx := context.Background()

	// Test successful operation
	operation := func() error {
		return nil
	}

	err := erm.ExecuteWithRetry(ctx, "test_operation", operation)
	assert.NoError(t, err)
}

// TestErrorRecoveryManager_ExecuteWithRetry_ErrorOnly_WithRetries tests error-only retry with failures
func TestErrorRecoveryManager_ExecuteWithRetry_ErrorOnly_WithRetries(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Register a retry policy
	policy := &RetryPolicy{
		MaxRetries:    2,
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		JitterEnabled: false,
	}
	erm.RegisterRetryPolicy("test_operation", policy)

	attempts := 0
	operation := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}

	err := erm.ExecuteWithRetry(context.Background(), "test_operation", operation)
	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

// TestErrorRecoveryManager_DefaultRetryPolicies tests default retry policies
func TestErrorRecoveryManager_DefaultRetryPolicies(t *testing.T) {
	policies := DefaultRetryPolicies()

	assert.NotEmpty(t, policies)
	assert.Contains(t, policies, "api_call")
	assert.Contains(t, policies, "database_operation")
	assert.Contains(t, policies, "redis_operation")
	assert.Contains(t, policies, "concurrent_operation")

	// Verify default values
	apiPolicy := policies["api_call"]
	assert.Equal(t, 3, apiPolicy.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, apiPolicy.InitialDelay)
	assert.Equal(t, 5*time.Second, apiPolicy.MaxDelay)
	assert.Equal(t, 2.0, apiPolicy.BackoffFactor)
	assert.True(t, apiPolicy.JitterEnabled)
}

// TestErrorRecoveryManager_ConcurrentOperations tests concurrent error recovery operations
func TestErrorRecoveryManager_ConcurrentOperations(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Register retry policy
	policy := &RetryPolicy{
		MaxRetries:    2,
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		JitterEnabled: false,
	}
	erm.RegisterRetryPolicy("concurrent_test", policy)

	var wg sync.WaitGroup
	results := make(chan bool, 10)

	// Test concurrent operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			operation := func() (interface{}, error) {
				if id%3 == 0 {
					return nil, errors.New("simulated failure")
				}
				return "success", nil
			}

			result := erm.ExecuteWithRecovery(context.Background(), "concurrent_test", operation, nil)
			results <- result.Success
		}(i)
	}

	wg.Wait()
	close(results)

	// Count successful operations
	successCount := 0
	for success := range results {
		if success {
			successCount++
		}
	}

	// Should have some successes (operations that didn't fail)
	assert.Greater(t, successCount, 0)
}

// TestErrorRecoveryManager_TimeHandling tests time handling in retry operations
func TestErrorRecoveryManager_TimeHandling(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Register retry policy
	policy := &RetryPolicy{
		MaxRetries:    1,
		InitialDelay:  50 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		JitterEnabled: false,
	}
	erm.RegisterRetryPolicy("time_test", policy)

	operation := func() (interface{}, error) {
		return nil, errors.New("always fails")
	}

	result := erm.ExecuteWithRecovery(context.Background(), "time_test", operation, nil)

	duration := result.Duration
	assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
	assert.Less(t, duration, 200*time.Millisecond) // Should be reasonable
}

// TestErrorRecoveryManager_StateManagement tests state management
func TestErrorRecoveryManager_StateManagement(t *testing.T) {
	logger := zaplogrus.New()
	erm := NewErrorRecoveryManager(logger)

	// Test initial state
	assert.False(t, erm.IsInDegradationMode())
	assert.True(t, erm.fallbackEnabled)

	// Test state transitions
	erm.EnableDegradationMode()
	assert.True(t, erm.IsInDegradationMode())

	erm.DisableDegradationMode()
	assert.False(t, erm.IsInDegradationMode())

	// Test concurrent state access
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			erm.EnableDegradationMode()
			erm.DisableDegradationMode()
		}()
	}
	wg.Wait()

	// Final state should be consistent
	assert.False(t, erm.IsInDegradationMode())
}
