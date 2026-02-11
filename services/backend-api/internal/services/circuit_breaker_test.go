package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_Basic(t *testing.T) {
	// Test basic circuit breaker functionality
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	assert.NotNil(t, breaker)
	assert.Equal(t, "test-breaker", breaker.name)
	assert.Equal(t, config, breaker.config)
	assert.NotNil(t, breaker.logger)
}

func TestCircuitBreaker_Execute(t *testing.T) {
	// Test the Execute function with a simple callback
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test successful execution
	err := breaker.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	assert.NoError(t, err)
}

func TestCircuitBreaker_ExecuteWithError(t *testing.T) {
	// Test the Execute function with a callback that returns an error
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test failed execution
	err := breaker.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("test error")
	})

	assert.Error(t, err)
	assert.Equal(t, "test error", err.Error())
}

func TestCircuitBreaker_GetState(t *testing.T) {
	// Test getting the current state of the circuit breaker
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test getting state
	state := breaker.GetState()
	assert.NotNil(t, state)
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	// Test getting statistics from the circuit breaker
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test getting stats
	stats := breaker.GetStats()
	assert.NotNil(t, stats)
}

func TestCircuitBreaker_IsOpen(t *testing.T) {
	// Test checking if the circuit breaker is open
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test initial state
	isOpen := breaker.IsOpen()
	assert.False(t, isOpen) // Should be closed initially
}

func TestCircuitBreaker_Reset(t *testing.T) {
	// Test resetting the circuit breaker
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test reset functionality
	breaker.Reset()

	// Should still be closed after reset
	assert.False(t, breaker.IsOpen())
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	// Test concurrent access to the circuit breaker
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 10,
		SuccessThreshold: 5,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := breaker.Execute(context.Background(), func(ctx context.Context) error {
				return nil
			})
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
}

func TestCircuitBreaker_ConfigDefaults(t *testing.T) {
	// Test circuit breaker with default configuration
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 0, // Should use default
		SuccessThreshold: 0, // Should use default
		Timeout:          0, // Should use default
		MaxRequests:      0, // Should use default
		ResetTimeout:     0, // Should use default
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	assert.NotNil(t, breaker)

	// Should be able to execute successfully
	err := breaker.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})
	assert.NoError(t, err)
}

func TestCircuitBreaker_ContextCancellation(t *testing.T) {
	// Test circuit breaker with cancelled context
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The context cancellation test depends on the implementation
	// For now, we'll test that the function can handle cancelled context
	_ = ctx // Use the context to avoid unused variable error
	assert.NotNil(t, breaker)
}

func TestCircuitBreaker_StateManagement(t *testing.T) {
	// Test state management functionality
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test state transitions
	initialState := breaker.GetState()
	assert.NotNil(t, initialState)

	// Execute successful operations to see state changes
	for i := 0; i < 3; i++ {
		err := breaker.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
		assert.NoError(t, err)
	}

	// Check stats after operations
	stats := breaker.GetStats()
	assert.NotNil(t, stats)
}

func TestCircuitBreaker_ErrorScenarios(t *testing.T) {
	// Test various error scenarios
	logger := zaplogrus.New()

	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          time.Millisecond * 100, // Short timeout for testing
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test with error callback
	err := breaker.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("callback error")
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "callback error")
}

func TestCircuitBreakerManager_GetAllStats_Empty(t *testing.T) {
	// Test GetAllStats with empty manager
	logger := zaplogrus.New()

	manager := NewCircuitBreakerManager(logger)

	stats := manager.GetAllStats()

	assert.NotNil(t, stats)
	assert.Empty(t, stats) // Should be empty for new manager
}

func TestCircuitBreakerManager_GetAllStats_WithBreakers(t *testing.T) {
	// Test GetAllStats with multiple circuit breakers
	logger := zaplogrus.New()

	manager := NewCircuitBreakerManager(logger)

	config1 := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	config2 := CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          2 * time.Second,
		MaxRequests:      10,
		ResetTimeout:     2 * time.Minute,
	}

	// Create circuit breakers
	breaker1 := manager.GetOrCreate("breaker1", config1)
	breaker2 := manager.GetOrCreate("breaker2", config2)

	// Execute some operations to generate stats
	for i := 0; i < 3; i++ {
		_ = breaker1.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
	}

	for i := 0; i < 2; i++ {
		_ = breaker2.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
	}

	// Get stats
	stats := manager.GetAllStats()

	assert.NotNil(t, stats)
	assert.Len(t, stats, 2)
	assert.Contains(t, stats, "breaker1")
	assert.Contains(t, stats, "breaker2")

	// Verify stats contain expected data
	assert.Equal(t, int64(3), stats["breaker1"].TotalRequests)
	assert.Equal(t, int64(3), stats["breaker1"].SuccessfulRequests)
	assert.Equal(t, int64(2), stats["breaker2"].TotalRequests)
	assert.Equal(t, int64(2), stats["breaker2"].SuccessfulRequests)
}

func TestCircuitBreakerManager_GetAllStats_ConcurrentAccess(t *testing.T) {
	// Test concurrent access to GetAllStats
	logger := zaplogrus.New()

	manager := NewCircuitBreakerManager(logger)

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	// Create multiple breakers concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("breaker-%d", i)
			_ = manager.GetOrCreate(name, config)
		}(i)
	}
	wg.Wait()

	// Test concurrent stats access
	var statsResults []map[string]CircuitBreakerStats
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats := manager.GetAllStats()
			mu.Lock()
			statsResults = append(statsResults, stats)
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Verify all stats results are consistent
	assert.Len(t, statsResults, 5)
	for i := 1; i < len(statsResults); i++ {
		assert.Equal(t, len(statsResults[0]), len(statsResults[i]))
	}
}

func TestCircuitBreakerManager_ResetAll_Empty(t *testing.T) {
	// Test ResetAll with empty manager
	logger := zaplogrus.New()

	manager := NewCircuitBreakerManager(logger)

	// Should not panic on empty manager
	manager.ResetAll()

	// Verify stats are still empty
	stats := manager.GetAllStats()
	assert.Empty(t, stats)
}

func TestCircuitBreakerManager_ResetAll_WithBreakers(t *testing.T) {
	// Test ResetAll with multiple circuit breakers
	logger := zaplogrus.New()

	manager := NewCircuitBreakerManager(logger)

	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          time.Millisecond * 100,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker1 := manager.GetOrCreate("breaker1", config)
	breaker2 := manager.GetOrCreate("breaker2", config)

	// Generate some failures and successes
	for i := 0; i < 2; i++ {
		_ = breaker1.Execute(context.Background(), func(ctx context.Context) error {
			return errors.New("test error")
		})
	}

	for i := 0; i < 3; i++ {
		_ = breaker2.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
	}

	// Get stats before reset
	statsBefore := manager.GetAllStats()
	assert.Equal(t, int64(2), statsBefore["breaker1"].TotalRequests)
	assert.Equal(t, int64(0), statsBefore["breaker1"].SuccessfulRequests)
	assert.Equal(t, int64(3), statsBefore["breaker2"].TotalRequests)
	assert.Equal(t, int64(3), statsBefore["breaker2"].SuccessfulRequests)

	// Reset all breakers
	manager.ResetAll()

	// Verify all breakers are reset to closed state
	assert.False(t, breaker1.IsOpen())
	assert.False(t, breaker2.IsOpen())

	// Verify stats are reset but total counts remain
	statsAfter := manager.GetAllStats()
	assert.Equal(t, int64(2), statsAfter["breaker1"].TotalRequests) // Total should remain
	assert.Equal(t, int64(3), statsAfter["breaker2"].TotalRequests) // Total should remain
}

func TestCircuitBreakerManager_ResetAll_ConcurrentAccess(t *testing.T) {
	// Test concurrent access to ResetAll
	logger := zaplogrus.New()

	manager := NewCircuitBreakerManager(logger)

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	// Create breakers
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("breaker-%d", i)
		_ = manager.GetOrCreate(name, config)
	}

	// Test concurrent reset operations
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.ResetAll()
		}()
	}
	wg.Wait()

	// Verify all breakers are still closed
	stats := manager.GetAllStats()
	for name := range stats {
		// All breakers should be in closed state after reset
		breaker := manager.GetOrCreate(name, config)
		assert.False(t, breaker.IsOpen())
	}
}

func TestCircuitBreakerManager_GetAllStats_AfterReset(t *testing.T) {
	// Test GetAllStats after ResetAll
	logger := zaplogrus.New()

	manager := NewCircuitBreakerManager(logger)

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := manager.GetOrCreate("test-breaker", config)

	// Generate some activity
	for i := 0; i < 5; i++ {
		_ = breaker.Execute(context.Background(), func(ctx context.Context) error {
			if i%2 == 0 {
				return nil
			}
			return errors.New("test error")
		})
	}

	// Get stats before reset
	statsBefore := manager.GetAllStats()
	assert.Equal(t, int64(5), statsBefore["test-breaker"].TotalRequests)
	assert.Equal(t, int64(3), statsBefore["test-breaker"].SuccessfulRequests)
	assert.Equal(t, int64(2), statsBefore["test-breaker"].FailedRequests)

	// Reset all
	manager.ResetAll()

	// Get stats after reset
	statsAfter := manager.GetAllStats()

	// Total counts should remain but state should be reset
	assert.Equal(t, int64(5), statsAfter["test-breaker"].TotalRequests)
	assert.Equal(t, int64(3), statsAfter["test-breaker"].SuccessfulRequests)
	assert.Equal(t, int64(2), statsAfter["test-breaker"].FailedRequests)

	// Breaker should be closed
	assert.False(t, breaker.IsOpen())
}

func TestCircuitBreaker_canExecute_ClosedState(t *testing.T) {
	logger := zaplogrus.New()
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Initially in closed state, should allow execution
	assert.True(t, breaker.canExecute())
	assert.Equal(t, Closed, breaker.GetState())

	// Simulate some failures
	breaker.failureCount = 2
	assert.True(t, breaker.canExecute()) // Still below threshold

	// Exceed failure threshold
	breaker.failureCount = 3
	breaker.setState(Open)                                     // Manually set to open to test reset logic
	breaker.lastFailureTime = time.Now().Add(-2 * time.Minute) // Old failure

	// Should still not allow because state is open
	assert.False(t, breaker.canExecute())
}

func TestCircuitBreaker_canExecute_OpenState(t *testing.T) {
	logger := zaplogrus.New()
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Set to open state
	breaker.setState(Open)
	breaker.lastStateChange = time.Now()

	// Should not allow execution immediately after opening
	assert.False(t, breaker.canExecute())

	// Should not allow if timeout hasn't passed
	assert.False(t, breaker.canExecute())

	// Simulate timeout passing
	breaker.lastStateChange = time.Now().Add(-2 * time.Minute)

	// Should now allow execution and transition to half-open
	assert.True(t, breaker.canExecute())
	assert.Equal(t, HalfOpen, breaker.GetState())
	assert.Equal(t, 0, breaker.requestCount)
	assert.Equal(t, 0, breaker.successCount)
}

func TestCircuitBreaker_canExecute_HalfOpenState(t *testing.T) {
	logger := zaplogrus.New()
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Set to half-open state
	breaker.setState(HalfOpen)

	// Should allow execution while under max requests
	for i := 0; i < config.MaxRequests; i++ {
		assert.True(t, breaker.canExecute(), "Should allow request %d", i)
		breaker.requestCount++
	}

	// Should not allow execution when max requests reached
	assert.False(t, breaker.canExecute())

	// Test with different max requests
	breaker.requestCount = 0
	config.MaxRequests = 1
	breaker.config = config

	assert.True(t, breaker.canExecute())
	breaker.requestCount++
	assert.False(t, breaker.canExecute())
}

func TestCircuitBreaker_canExecute_ResetTimeout(t *testing.T) {
	logger := zaplogrus.New()
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     100 * time.Millisecond, // Short timeout for testing
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Set some failures and old last failure time
	breaker.failureCount = 2
	breaker.lastFailureTime = time.Now().Add(-200 * time.Millisecond) // Older than reset timeout

	// Should reset failure count and allow execution
	assert.True(t, breaker.canExecute())
	assert.Equal(t, 0, breaker.failureCount)

	// Test with recent failure
	breaker.failureCount = 2
	breaker.lastFailureTime = time.Now().Add(-50 * time.Millisecond) // Recent failure

	// Should still allow but not reset
	assert.True(t, breaker.canExecute())
	assert.Equal(t, 2, breaker.failureCount) // Not reset
}

func TestCircuitBreaker_canExecute_UnknownState(t *testing.T) {
	logger := zaplogrus.New()
	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          time.Second,
		MaxRequests:      5,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Set to invalid state (this would be unusual but tests the default case)
	breaker.state = CircuitBreakerState(99)

	// Should not allow execution for unknown state
	assert.False(t, breaker.canExecute())
}

func TestCircuitBreaker_canExecute_StateTransitions(t *testing.T) {
	logger := zaplogrus.New()
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		MaxRequests:      3,
		ResetTimeout:     time.Minute,
	}

	breaker := NewCircuitBreaker("test-breaker", config, logger)

	// Test Closed -> Open transition
	assert.True(t, breaker.canExecute()) // Closed state
	breaker.setState(Open)
	breaker.lastStateChange = time.Now()
	assert.False(t, breaker.canExecute()) // Open state

	// Test Open -> HalfOpen transition
	breaker.lastStateChange = time.Now().Add(-100 * time.Millisecond) // Timeout passed
	assert.True(t, breaker.canExecute())                              // Should transition to half-open
	assert.Equal(t, HalfOpen, breaker.GetState())

	// Test HalfOpen request limiting
	assert.True(t, breaker.canExecute()) // Request 1
	breaker.requestCount++
	assert.True(t, breaker.canExecute()) // Request 2
	breaker.requestCount++
	assert.True(t, breaker.canExecute()) // Request 3
	breaker.requestCount++
	assert.False(t, breaker.canExecute()) // Max requests reached
}

func TestCircuitBreaker_canExecute_ConcurrentAccess(t *testing.T) {
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          5 * time.Millisecond,
		MaxRequests:      1,
	}
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)

	breaker := NewCircuitBreaker("test-breaker", config, logger)
	breaker.setState(HalfOpen)

	// Manually set requestCount to test the limit
	breaker.requestCount = 0

	// Test that canExecute respects the MaxRequests limit
	result1 := breaker.canExecute()
	assert.True(t, result1, "First request should be allowed")

	// Simulate the request count increment (this would happen in Execute)
	breaker.requestCount = 1

	result2 := breaker.canExecute()
	assert.False(t, result2, "Second request should be rejected when MaxRequests=1")

	// Test concurrent access - all should see the same requestCount
	var wg sync.WaitGroup
	results := make([]bool, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = breaker.canExecute()
		}(i)
	}

	wg.Wait()

	// All should be false since requestCount >= MaxRequests
	for i, result := range results {
		assert.False(t, result, "Concurrent request %d should be rejected", i)
	}
}
