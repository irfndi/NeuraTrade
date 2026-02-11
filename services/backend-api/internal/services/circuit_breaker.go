package services

import (
	"context"
	"errors"
	"sync"
	"time"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
)

// CircuitBreakerState represents the current state of the circuit breaker.
type CircuitBreakerState int

const (
	// Closed means the circuit is functioning normally.
	Closed CircuitBreakerState = iota
	// Open means the circuit is broken and requests are failing fast.
	Open
	// HalfOpen means the circuit is testing if the upstream service is back online.
	HalfOpen
)

// CircuitBreakerConfig holds configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening.
	FailureThreshold int `json:"failure_threshold"`
	// SuccessThreshold is the number of successes to close from half-open.
	SuccessThreshold int `json:"success_threshold"`
	// Timeout is the time to wait before trying half-open.
	Timeout time.Duration `json:"timeout"`
	// MaxRequests is the max requests allowed in half-open state.
	MaxRequests int `json:"max_requests"`
	// ResetTimeout is the time to reset failure count.
	ResetTimeout time.Duration `json:"reset_timeout"`
}

// CircuitBreakerStats holds statistics for the circuit breaker.
type CircuitBreakerStats struct {
	// TotalRequests is the total number of requests.
	TotalRequests int64 `json:"total_requests"`
	// SuccessfulRequests is the number of successful requests.
	SuccessfulRequests int64 `json:"successful_requests"`
	// FailedRequests is the number of failed requests.
	FailedRequests int64 `json:"failed_requests"`
	// LastFailureTime is the time of the last failure.
	LastFailureTime time.Time `json:"last_failure_time"`
	// LastSuccessTime is the time of the last success.
	LastSuccessTime time.Time `json:"last_success_time"`
	// StateChanges is the number of times state has changed.
	StateChanges int64 `json:"state_changes"`
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	name            string
	config          CircuitBreakerConfig
	logger          *zaplogrus.Logger
	mu              sync.RWMutex
	state           CircuitBreakerState
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	lastStateChange time.Time
	requestCount    int
	stats           CircuitBreakerStats
}

// NewCircuitBreaker creates a new circuit breaker.
//
// Parameters:
//
//	name: Breaker name.
//	config: Configuration.
//	logger: Logger instance.
//
// Returns:
//
//	*CircuitBreaker: Initialized breaker.
func NewCircuitBreaker(name string, config CircuitBreakerConfig, logger *zaplogrus.Logger) *CircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 3
	}
	if config.Timeout <= 0 {
		config.Timeout = 60 * time.Second
	}
	if config.MaxRequests <= 0 {
		config.MaxRequests = 10
	}
	if config.ResetTimeout <= 0 {
		config.ResetTimeout = 300 * time.Second
	}

	return &CircuitBreaker{
		name:            name,
		config:          config,
		logger:          logger,
		state:           Closed,
		lastStateChange: time.Now(),
	}
}

// Execute runs the given function with circuit breaker protection.
//
// Parameters:
//
//	ctx: Context.
//	fn: Function to execute.
//
// Returns:
//
//	error: Error from function or circuit breaker.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.stats.TotalRequests++

	// Check if circuit breaker should allow the request
	if !cb.canExecute() {
		cb.logger.WithFields(zaplogrus.Fields{
			"circuit_breaker": cb.name,
			"state":           cb.getStateName(),
			"failure_count":   cb.failureCount,
		}).Warn("Circuit breaker is open, rejecting request")
		return errors.New("circuit breaker is open")
	}

	// Execute the function
	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	// Record the result
	if err != nil {
		cb.onFailure(err, duration)
	} else {
		cb.onSuccess(duration)
	}

	return err
}

// canExecute determines if the circuit breaker should allow execution
func (cb *CircuitBreaker) canExecute() bool {
	now := time.Now()

	switch cb.state {
	case Closed:
		// Reset failure count if enough time has passed
		if now.Sub(cb.lastFailureTime) > cb.config.ResetTimeout {
			cb.failureCount = 0
		}
		return true

	case Open:
		// Check if we should transition to half-open
		if now.Sub(cb.lastStateChange) > cb.config.Timeout {
			cb.setState(HalfOpen)
			cb.requestCount = 0
			cb.successCount = 0
			return true
		}
		return false

	case HalfOpen:
		// Allow limited requests in half-open state
		return cb.requestCount < cb.config.MaxRequests

	default:
		return false
	}
}

// onSuccess handles successful execution
func (cb *CircuitBreaker) onSuccess(duration time.Duration) {
	cb.stats.SuccessfulRequests++
	cb.stats.LastSuccessTime = time.Now()

	switch cb.state {
	case Closed:
		// Reset failure count on success
		cb.failureCount = 0

	case HalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.setState(Closed)
			cb.failureCount = 0
			cb.successCount = 0
			cb.requestCount = 0
		}
	}

	cb.logger.WithFields(zaplogrus.Fields{
		"circuit_breaker": cb.name,
		"state":           cb.getStateName(),
		"duration_ms":     duration.Milliseconds(),
		"success_count":   cb.successCount,
	}).Debug("Circuit breaker: successful execution")
}

// onFailure handles failed execution
func (cb *CircuitBreaker) onFailure(err error, duration time.Duration) {
	cb.stats.FailedRequests++
	cb.stats.LastFailureTime = time.Now()
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case Closed:
		cb.failureCount++
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.setState(Open)
		}

	case HalfOpen:
		// Any failure in half-open state should open the circuit
		cb.setState(Open)
		cb.failureCount++
		cb.successCount = 0
		cb.requestCount = 0
	}

	cb.logger.WithFields(zaplogrus.Fields{
		"circuit_breaker": cb.name,
		"state":           cb.getStateName(),
		"error":           err.Error(),
		"duration_ms":     duration.Milliseconds(),
		"failure_count":   cb.failureCount,
	}).Warn("Circuit breaker: failed execution")
}

// setState changes the circuit breaker state
func (cb *CircuitBreaker) setState(newState CircuitBreakerState) {
	if cb.state != newState {
		oldState := cb.state
		cb.state = newState
		cb.lastStateChange = time.Now()
		cb.stats.StateChanges++

		cb.logger.WithFields(zaplogrus.Fields{
			"circuit_breaker": cb.name,
			"old_state":       cb.getStateNameForState(oldState),
			"new_state":       cb.getStateName(),
			"failure_count":   cb.failureCount,
		}).Info("Circuit breaker state changed")
	}
}

// GetState returns the current state of the circuit breaker.
//
// Returns:
//
//	CircuitBreakerState: Current state.
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns the current statistics.
//
// Returns:
//
//	CircuitBreakerStats: Stats.
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.stats
}

// IsOpen returns true if the circuit breaker is open.
//
// Returns:
//
//	bool: True if open.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state == Open
}

// Reset manually resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.setState(Closed)
	cb.failureCount = 0
	cb.successCount = 0
	cb.requestCount = 0

	cb.logger.WithField("circuit_breaker", cb.name).Info("Circuit breaker manually reset")
}

// getStateName returns the string representation of the current state
func (cb *CircuitBreaker) getStateName() string {
	return cb.getStateNameForState(cb.state)
}

// getStateNameForState returns the string representation of a given state
func (cb *CircuitBreaker) getStateNameForState(state CircuitBreakerState) string {
	switch state {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerManager manages multiple circuit breakers.
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	logger   *zaplogrus.Logger
	mu       sync.RWMutex
}

// NewCircuitBreakerManager creates a new circuit breaker manager.
//
// Parameters:
//
//	logger: Logger instance.
//
// Returns:
//
//	*CircuitBreakerManager: Initialized manager.
func NewCircuitBreakerManager(logger *zaplogrus.Logger) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger,
	}
}

// GetOrCreate gets an existing circuit breaker or creates a new one.
//
// Parameters:
//
//	name: Breaker name.
//	config: Configuration.
//
// Returns:
//
//	*CircuitBreaker: The breaker.
func (cbm *CircuitBreakerManager) GetOrCreate(name string, config CircuitBreakerConfig) *CircuitBreaker {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	if breaker, exists := cbm.breakers[name]; exists {
		return breaker
	}

	breaker := NewCircuitBreaker(name, config, cbm.logger)
	cbm.breakers[name] = breaker
	return breaker
}

// GetAllStats returns statistics for all circuit breakers.
//
// Returns:
//
//	map[string]CircuitBreakerStats: Map of stats.
func (cbm *CircuitBreakerManager) GetAllStats() map[string]CircuitBreakerStats {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	stats := make(map[string]CircuitBreakerStats)
	for name, breaker := range cbm.breakers {
		stats[name] = breaker.GetStats()
	}
	return stats
}

// ResetAll resets all circuit breakers.
func (cbm *CircuitBreakerManager) ResetAll() {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	for _, breaker := range cbm.breakers {
		breaker.Reset()
	}
}
