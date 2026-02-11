# Error Recovery and Resilience Documentation

This document provides comprehensive guidance on the error recovery mechanisms implemented throughout the NeuraTrade Go application to ensure robust operation under various failure conditions.

## Overview

The application implements multiple layers of error recovery and resilience patterns:

1. **Circuit Breaker Pattern** - Prevents cascading failures
2. **Retry Logic with Exponential Backoff** - Handles transient failures
3. **Graceful Degradation** - Fallback mechanisms for concurrent operations
4. **Resource Management** - Prevents resource leaks and manages cleanup
5. **Timeout Handling** - Comprehensive timeout management
6. **Health Monitoring** - Continuous health checks and monitoring

## Circuit Breaker Pattern

### Implementation

Circuit breakers are implemented in the `internal/services/error_recovery.go` file and used throughout the application to prevent cascading failures.

#### Configuration

```go
type CircuitBreakerConfig struct {
    FailureThreshold int           // Number of failures before opening
    SuccessThreshold int           // Number of successes to close
    Timeout          time.Duration // Time to wait before half-open
    MaxRequests      int           // Max requests in half-open state
    ResetTimeout     time.Duration // Time to reset failure count
}
```

#### Usage Examples

**Redis Operations:**
```go
// Redis client with circuit breaker
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
    var result string
    err := r.errorRecoveryManager.ExecuteWithRetry(ctx, "redis_operation", func() error {
        var getErr error
        result, getErr = r.client.Get(ctx, key).Result()
        return getErr
    })
    return result, err
}
```

**CCXT Service Calls:**
```go
// CCXT service with circuit breaker
func (s *Service) FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]models.MarketPrice, error) {
    var resp *TickersResponse
    err := s.getCircuitBreaker("market_data").Execute(ctx, func(ctx context.Context) error {
        var err error
        resp, err = s.client.GetTickers(ctx, req)
        return err
    })
    return marketData, err
}
```

### Circuit Breaker States

- **Closed**: Normal operation, requests pass through
- **Open**: Circuit is open, requests fail immediately
- **Half-Open**: Limited requests allowed to test recovery

## Retry Logic with Exponential Backoff

### Implementation

Retry logic is implemented in the `ErrorRecoveryManager` with configurable policies for different operation types.

#### Retry Policies

```go
var DefaultRetryPolicies = map[string]RetryPolicy{
    "api_call": {
        MaxRetries:    3,
        BaseDelay:     100 * time.Millisecond,
        MaxDelay:      5 * time.Second,
        BackoffFactor: 2.0,
        Jitter:        true,
    },
    "database_operation": {
        MaxRetries:    5,
        BaseDelay:     50 * time.Millisecond,
        MaxDelay:      2 * time.Second,
        BackoffFactor: 1.5,
        Jitter:        true,
    },
    "redis_operation": {
        MaxRetries:    3,
        BaseDelay:     25 * time.Millisecond,
        MaxDelay:      1 * time.Second,
        BackoffFactor: 2.0,
        Jitter:        true,
    },
}
```

#### Usage

```go
// Execute with retry
err := c.errorRecoveryManager.ExecuteWithRetry(ctx, "symbol_fetch", func() error {
    activeSymbols, fetchErr = c.fetchAndCacheSymbols(exchangeID)
    return fetchErr
})
```

## Graceful Degradation

### Concurrent to Sequential Fallback

The application implements graceful degradation for concurrent operations, falling back to sequential processing when concurrent operations fail.

#### Symbol Collection Example

```go
func (c *CollectorService) getMultiExchangeSymbolsConcurrent(exchanges []string) (map[string]int, error) {
    // Try concurrent approach first
    result, err := c.tryGetSymbolsConcurrent(ctx, exchanges, maxConcurrent, minExchanges)
    if err != nil {
        log.Printf("Concurrent symbol collection failed: %v. Falling back to sequential processing...", err)
        // Fallback to sequential processing
        return c.getSymbolsSequential(exchanges, minExchanges)
    }
    return result, nil
}
```

#### Failure Thresholds

- **50% Failure Rate**: Triggers fallback to sequential processing
- **Context Cancellation**: Immediate fallback on timeout
- **Resource Exhaustion**: Automatic degradation

## Resource Management

### Resource Manager

The `ResourceManager` in `internal/services/resource_manager.go` provides comprehensive resource lifecycle management.

#### Features

- **Automatic Cleanup**: Idle resource detection and cleanup
- **Leak Detection**: Identifies potential resource leaks
- **Resource Tracking**: Monitors resource usage and statistics
- **Graceful Shutdown**: Ensures proper cleanup on application shutdown

#### Usage

```go
// Register a resource
c.resourceManager.RegisterResource(workerID, ResourceTypeGoroutine, func() error {
    log.Printf("Cleaning up worker for exchange %s", worker.Exchange)
    worker.IsRunning = false
    return nil
})
defer c.resourceManager.UnregisterResource(workerID)
```

### Resource Types

- `ResourceTypeGoroutine`: Goroutine management
- `ResourceTypeConnection`: Network connections
- `ResourceTypeFile`: File handles
- `ResourceTypeMemory`: Memory allocations

## Timeout Handling

### Timeout Manager

The `TimeoutManager` in `internal/services/timeout_manager.go` provides comprehensive timeout management for all operations.

#### Configuration

```go
type TimeoutConfig struct {
    APICall         time.Duration // 10s
    DatabaseQuery   time.Duration // 5s
    RedisOperation  time.Duration // 2s
    ConcurrentOp    time.Duration // 15s
    HealthCheck     time.Duration // 3s
    Backfill        time.Duration // 60s
    SymbolFetch     time.Duration // 20s
    MarketData      time.Duration // 8s
}
```

#### Usage

```go
// Create operation context with timeout
operationCtx := c.timeoutManager.CreateOperationContext("worker_collection", operationID)
defer c.timeoutManager.CompleteOperation(operationID)

// Execute operation with timeout
result, err := c.timeoutManager.ExecuteWithTimeout("api_call", operationID, func(ctx context.Context) (interface{}, error) {
    return c.performOperation(ctx)
})
```

## Health Monitoring

### Performance Monitor

The `PerformanceMonitor` provides continuous health monitoring and metrics collection.

#### Monitored Metrics

- **Worker Health**: Active workers, error counts, last update times
- **System Resources**: Memory usage, goroutine count, CPU usage
- **Operation Performance**: Success rates, response times, throughput
- **Cache Performance**: Hit rates, miss rates, eviction counts

#### Health Checks

```go
// Worker health check
c.performanceMonitor.RecordWorkerHealth(worker.Exchange, worker.IsRunning, worker.ErrorCount)

// Service health check
func (c *CollectorService) IsHealthy() bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    if len(c.workers) == 0 {
        return false
    }
    
    // Check if at least 50% of workers are running
    runningWorkers := 0
    for _, worker := range c.workers {
        if worker.IsRunning {
            runningWorkers++
        }
    }
    
    return float64(runningWorkers)/float64(len(c.workers)) >= 0.5
}
```

## Error Aggregation and Reporting

### Error Collection

Errors are collected and aggregated across all concurrent operations for comprehensive reporting.

#### Error Tracking

```go
// Collect errors from concurrent operations
var errors []error
for result := range resultChan {
    if result.err != nil {
        errors = append(errors, result.err)
        log.Printf("Operation failed: %v", result.err)
    }
}

// Report aggregated errors
if len(errors) > 0 {
    log.Printf("Operation completed with %d errors out of %d total operations", len(errors), totalOperations)
    for i, err := range errors {
        log.Printf("Error %d: %v", i+1, err)
    }
}
```

### Error Categories

- **Transient Errors**: Network timeouts, temporary service unavailability
- **Permanent Errors**: Invalid credentials, malformed requests
- **Resource Errors**: Memory exhaustion, connection limits
- **Logic Errors**: Invalid state, data corruption

## Best Practices

### 1. Context Propagation

Always propagate context through the call chain to enable proper cancellation and timeout handling.

```go
func (c *CollectorService) processData(ctx context.Context) error {
    // Always check context before expensive operations
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    
    // Pass context to downstream operations
    return c.performOperation(ctx)
}
```

### 2. Resource Cleanup

Always use defer statements for resource cleanup and register resources with the ResourceManager.

```go
func (c *CollectorService) performOperation() error {
    // Register operation
    operationID := fmt.Sprintf("operation_%d", time.Now().UnixNano())
    c.resourceManager.RegisterOperation(operationID, "processing", 1)
    defer c.resourceManager.UnregisterOperation(operationID)
    
    // Perform operation
    return c.doWork()
}
```

### 3. Error Wrapping

Wrap errors with context to provide better debugging information.

```go
if err != nil {
    return fmt.Errorf("failed to fetch symbols for exchange %s: %w", exchangeID, err)
}
```

### 4. Monitoring Integration

Integrate with monitoring systems to track error rates and performance metrics.

```go
// Record metrics
c.performanceMonitor.RecordOperation("symbol_fetch", time.Since(startTime), err == nil)
```

## Configuration

### Environment Variables

```bash
# Circuit Breaker Configuration
CIRCUIT_BREAKER_FAILURE_THRESHOLD=5
CIRCUIT_BREAKER_SUCCESS_THRESHOLD=3
CIRCUIT_BREAKER_TIMEOUT=60s

# Retry Configuration
RETRY_MAX_ATTEMPTS=3
RETRY_BASE_DELAY=100ms
RETRY_MAX_DELAY=5s

# Timeout Configuration
API_CALL_TIMEOUT=10s
DATABASE_QUERY_TIMEOUT=5s
REDIS_OPERATION_TIMEOUT=2s
```

### Configuration Files

```yaml
# config/error_recovery.yaml
error_recovery:
  circuit_breaker:
    failure_threshold: 5
    success_threshold: 3
    timeout: 60s
    max_requests: 10
    reset_timeout: 300s
  
  retry:
    max_retries: 3
    base_delay: 100ms
    max_delay: 5s
    backoff_factor: 2.0
    jitter: true
  
  timeouts:
    api_call: 10s
    database_query: 5s
    redis_operation: 2s
    concurrent_op: 15s
```

## Troubleshooting

### Common Issues

1. **Circuit Breaker Stuck Open**
   - Check service health
   - Verify network connectivity
   - Review error logs for root cause

2. **High Retry Rates**
   - Monitor service performance
   - Check for resource contention
   - Review retry policy configuration

3. **Resource Leaks**
   - Monitor resource usage metrics
   - Check for missing cleanup code
   - Review goroutine counts

4. **Timeout Issues**
   - Review timeout configurations
   - Check for blocking operations
   - Monitor operation performance

### Debugging

```go
// Enable debug logging
logger.SetLevel(logrus.DebugLevel)

// Monitor circuit breaker state
state := circuitBreaker.GetState()
log.Printf("Circuit breaker state: %s", state)

// Check active operations
activeOps := timeoutManager.GetActiveOperations()
log.Printf("Active operations: %v", activeOps)
```

## Monitoring and Alerting

### Key Metrics to Monitor

- Circuit breaker state changes
- Retry attempt rates
- Operation timeout rates
- Resource usage trends
- Error rates by operation type

### Alert Thresholds

- Circuit breaker open > 5 minutes
- Retry rate > 50% for any operation
- Timeout rate > 10% for any operation
- Resource usage > 80% of limits
- Error rate > 5% for critical operations

## Conclusion

The error recovery mechanisms implemented in this application provide comprehensive resilience against various failure modes. By following the patterns and best practices outlined in this document, developers can ensure robust operation and quick recovery from failures.

For additional support or questions, refer to the code comments in the respective service files or contact the development team.