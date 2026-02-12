package services

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// ResourceOptimizer dynamically adjusts worker pool sizes and concurrency limits
// based on system resources and performance metrics.
type ResourceOptimizer struct {
	mu                   sync.RWMutex
	cpuCores             int
	memoryGB             float64
	currentCPUUsage      float64
	currentMemoryUsage   float64
	optimalConcurrency   OptimalConcurrency
	lastOptimization     time.Time
	optimizationInterval time.Duration
	performanceHistory   []PerformanceSnapshot
	maxHistorySize       int
	adaptiveMode         bool
	logger               *slog.Logger
}

// OptimalConcurrency holds dynamically calculated concurrency limits.
type OptimalConcurrency struct {
	// MaxWorkers is the optimal number of workers.
	MaxWorkers int `json:"max_workers"`
	// MaxConcurrentSymbols is the limit for symbol processing.
	MaxConcurrentSymbols int `json:"max_concurrent_symbols"`
	// MaxConcurrentBackfill is the limit for backfill operations.
	MaxConcurrentBackfill int `json:"max_concurrent_backfill"`
	// MaxConcurrentWrites is the limit for database writes.
	MaxConcurrentWrites int `json:"max_concurrent_writes"`
	// MaxCircuitBreakerCalls is the limit for circuit breaker requests.
	MaxCircuitBreakerCalls int `json:"max_circuit_breaker_calls"`
	// WorkerPoolUtilization is the target utilization rate.
	WorkerPoolUtilization float64 `json:"worker_pool_utilization"`
	// MemoryThreshold is the memory usage threshold for optimization.
	MemoryThreshold float64 `json:"memory_threshold"`
	// CPUThreshold is the CPU usage threshold for optimization.
	CPUThreshold float64 `json:"cpu_threshold"`
}

// PerformanceSnapshot captures system performance at a point in time.
type PerformanceSnapshot struct {
	// Timestamp is the snapshot time.
	Timestamp time.Time `json:"timestamp"`
	// CPUUsage is the CPU usage percentage.
	CPUUsage float64 `json:"cpu_usage"`
	// MemoryUsage is the memory usage percentage.
	MemoryUsage float64 `json:"memory_usage"`
	// Goroutines is the number of goroutines.
	Goroutines int `json:"goroutines"`
	// ActiveOperations is the number of active operations.
	ActiveOperations int `json:"active_operations"`
	// Throughput is the operations per second.
	Throughput float64 `json:"throughput"`
	// ErrorRate is the error percentage.
	ErrorRate float64 `json:"error_rate"`
	// ResponseTime is the average response time in ms.
	ResponseTime float64 `json:"response_time_ms"`
}

// ResourceOptimizerConfig holds configuration for the resource optimizer.
type ResourceOptimizerConfig struct {
	// OptimizationInterval is the interval between optimizations.
	OptimizationInterval time.Duration `yaml:"optimization_interval" default:"5m"`
	// AdaptiveMode enables adaptive optimization.
	AdaptiveMode bool `yaml:"adaptive_mode" default:"true"`
	// MaxHistorySize is the maximum number of snapshots to keep.
	MaxHistorySize int `yaml:"max_history_size" default:"100"`
	// CPUThreshold is the CPU usage threshold.
	CPUThreshold float64 `yaml:"cpu_threshold" default:"80.0"`
	// MemoryThreshold is the memory usage threshold.
	MemoryThreshold float64 `yaml:"memory_threshold" default:"85.0"`
	// MinWorkers is the minimum number of workers.
	MinWorkers int `yaml:"min_workers" default:"2"`
	// MaxWorkers is the maximum number of workers.
	MaxWorkers int `yaml:"max_workers" default:"20"`
}

// NewResourceOptimizer creates a new resource optimizer.
//
// Parameters:
//
//	config: Optimizer configuration.
//
// Returns:
//
//	*ResourceOptimizer: Initialized optimizer.
func NewResourceOptimizer(config ResourceOptimizerConfig) *ResourceOptimizer {
	// Apply default values if not provided
	if config.OptimizationInterval == 0 {
		config.OptimizationInterval = 5 * time.Minute
	}
	if config.MaxHistorySize == 0 {
		config.MaxHistorySize = 100
	}
	if config.CPUThreshold == 0 {
		config.CPUThreshold = 80.0
	}
	if config.MemoryThreshold == 0 {
		config.MemoryThreshold = 85.0
	}
	if config.MinWorkers == 0 {
		config.MinWorkers = 2
	}
	if config.MaxWorkers == 0 {
		config.MaxWorkers = 20
	}

	// Initialize logger with fallback for tests
	logger := telemetry.Logger()

	ro := &ResourceOptimizer{
		cpuCores:             runtime.NumCPU(),
		optimizationInterval: config.OptimizationInterval,
		maxHistorySize:       config.MaxHistorySize,
		adaptiveMode:         config.AdaptiveMode,
		performanceHistory:   make([]PerformanceSnapshot, 0), // Don't pre-allocate for tests
		logger:               logger,
	}

	// Get initial memory info
	if memInfo, err := mem.VirtualMemory(); err == nil {
		ro.memoryGB = float64(memInfo.Total) / (1024 * 1024 * 1024)
	} else {
		ro.logger.Warn("Could not get memory info, using default", "error", err)
		observability.CaptureExceptionWithContext(context.Background(), err, "resource_optimizer.memory_info", map[string]interface{}{
			"default_memory_gb": 8.0,
		})
		ro.memoryGB = 8.0 // Default to 8GB
	}

	// Calculate initial optimal concurrency
	ro.calculateOptimalConcurrency(config)

	// Seed performance history with an initial snapshot for baseline metrics
	ro.RecordPerformanceSnapshot(0, 0, 0, 0)

	ro.logger.Info("Resource Optimizer initialized",
		"cpu_cores", ro.cpuCores,
		"memory_gb", ro.memoryGB,
		"adaptive_mode", ro.adaptiveMode)

	return ro
}

// calculateOptimalConcurrency calculates optimal concurrency limits based on system resources
func (ro *ResourceOptimizer) calculateOptimalConcurrency(config ResourceOptimizerConfig) {
	_, span := observability.StartSpan(context.Background(), "resource.optimize", "ResourceOptimizer.calculateOptimalConcurrency")
	defer observability.FinishSpan(span, nil)

	ro.mu.Lock()
	defer ro.mu.Unlock()

	// Base calculations on CPU cores and memory
	baseWorkers := ro.cpuCores * 2 // Start with 2x CPU cores
	if baseWorkers < config.MinWorkers {
		baseWorkers = config.MinWorkers
	}
	if baseWorkers > config.MaxWorkers {
		baseWorkers = config.MaxWorkers
	}

	// Adjust based on memory (reduce if low memory)
	memoryFactor := 1.0
	if ro.memoryGB < 4.0 {
		memoryFactor = 0.5 // Reduce by 50% for low memory systems
	} else if ro.memoryGB < 8.0 {
		memoryFactor = 0.75 // Reduce by 25% for medium memory systems
	}

	// Adjust based on current system load if available
	loadFactor := 1.0
	if ro.currentCPUUsage > config.CPUThreshold {
		loadFactor = 0.7 // Reduce by 30% if CPU is high
	} else if ro.currentMemoryUsage > config.MemoryThreshold {
		loadFactor = 0.8 // Reduce by 20% if memory is high
	}

	// Calculate optimal values
	maxWorkers := int(float64(baseWorkers) * memoryFactor * loadFactor)
	if maxWorkers < config.MinWorkers {
		maxWorkers = config.MinWorkers
	}

	// Calculate concurrent limits with proper bounds
	maxConcurrentBackfill := maxWorkers / 2
	if maxConcurrentBackfill > 10 {
		maxConcurrentBackfill = 10
	}

	maxConcurrentWrites := maxWorkers / 3
	if maxConcurrentWrites > 15 {
		maxConcurrentWrites = 15
	}

	ro.optimalConcurrency = OptimalConcurrency{
		MaxWorkers:             maxWorkers,
		MaxConcurrentSymbols:   maxWorkers,            // Same as workers for symbol fetching
		MaxConcurrentBackfill:  maxConcurrentBackfill, // Half of workers, max 10
		MaxConcurrentWrites:    maxConcurrentWrites,   // Third of workers, max 15
		MaxCircuitBreakerCalls: maxWorkers * 2,        // 2x workers for circuit breaker
		WorkerPoolUtilization:  0.8,                   // Target 80% utilization
		MemoryThreshold:        config.MemoryThreshold,
		CPUThreshold:           config.CPUThreshold,
	}

	span.SetData("max_workers", ro.optimalConcurrency.MaxWorkers)
	span.SetData("max_concurrent_symbols", ro.optimalConcurrency.MaxConcurrentSymbols)
	span.SetData("max_concurrent_backfill", ro.optimalConcurrency.MaxConcurrentBackfill)
	span.SetData("max_concurrent_writes", ro.optimalConcurrency.MaxConcurrentWrites)
	span.SetData("max_circuit_breaker_calls", ro.optimalConcurrency.MaxCircuitBreakerCalls)
	span.SetData("adaptive_mode", ro.adaptiveMode)
	ro.logger.Info("Calculated optimal concurrency",
		"max_workers", ro.optimalConcurrency.MaxWorkers,
		"max_concurrent_symbols", ro.optimalConcurrency.MaxConcurrentSymbols,
		"max_concurrent_backfill", ro.optimalConcurrency.MaxConcurrentBackfill,
		"max_concurrent_writes", ro.optimalConcurrency.MaxConcurrentWrites,
		"max_circuit_breaker_calls", ro.optimalConcurrency.MaxCircuitBreakerCalls)
}

// GetOptimalConcurrency returns the current optimal concurrency settings.
//
// Returns:
//
//	OptimalConcurrency: Concurrency settings.
func (ro *ResourceOptimizer) GetOptimalConcurrency() OptimalConcurrency {
	ro.mu.RLock()
	defer ro.mu.RUnlock()
	return ro.optimalConcurrency
}

// UpdateSystemMetrics updates current system resource usage.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	error: Error if metrics update fails.
func (ro *ResourceOptimizer) UpdateSystemMetrics(ctx context.Context) error {
	spanCtx, span := observability.StartSpan(ctx, "resource.metrics", "ResourceOptimizer.UpdateSystemMetrics")
	var err error
	defer func() {
		observability.FinishSpan(span, err)
	}()

	// Get CPU usage
	cpuPercent, err := cpu.PercentWithContext(ctx, time.Second, false)
	if err != nil {
		observability.CaptureExceptionWithContext(spanCtx, err, "resource_optimizer.cpu_usage", map[string]interface{}{
			"cpu_cores": ro.cpuCores,
		})
		return fmt.Errorf("failed to get CPU usage: %w", err)
	}
	if len(cpuPercent) > 0 {
		ro.mu.Lock()
		ro.currentCPUUsage = cpuPercent[0]
		ro.mu.Unlock()
		span.SetData("cpu_usage", ro.currentCPUUsage)
	}

	// Get memory usage
	memInfo, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		observability.CaptureExceptionWithContext(spanCtx, err, "resource_optimizer.memory_usage", map[string]interface{}{
			"memory_gb": ro.memoryGB,
		})
		return fmt.Errorf("failed to get memory usage: %w", err)
	}
	ro.mu.Lock()
	ro.currentMemoryUsage = memInfo.UsedPercent
	ro.mu.Unlock()
	span.SetData("memory_usage", ro.currentMemoryUsage)

	return nil
}

// RecordPerformanceSnapshot records current performance metrics.
//
// Parameters:
//
//	activeOps: Active operations count.
//	throughput: Throughput rate.
//	errorRate: Error rate.
//	responseTime: Response time.
func (ro *ResourceOptimizer) RecordPerformanceSnapshot(activeOps int, throughput, errorRate, responseTime float64) {
	ro.mu.Lock()
	defer ro.mu.Unlock()

	snapshot := PerformanceSnapshot{
		Timestamp:        time.Now(),
		CPUUsage:         ro.currentCPUUsage,
		MemoryUsage:      ro.currentMemoryUsage,
		Goroutines:       runtime.NumGoroutine(),
		ActiveOperations: activeOps,
		Throughput:       throughput,
		ErrorRate:        errorRate,
		ResponseTime:     responseTime,
	}

	// Add to history
	ro.performanceHistory = append(ro.performanceHistory, snapshot)

	// Trim history if too large
	if len(ro.performanceHistory) > ro.maxHistorySize {
		ro.performanceHistory = ro.performanceHistory[1:]
	}
}

// OptimizeIfNeeded checks if optimization is needed and performs it.
//
// Parameters:
//
//	config: Optimization configuration.
//
// Returns:
//
//	bool: True if optimization was performed.
func (ro *ResourceOptimizer) OptimizeIfNeeded(config ResourceOptimizerConfig) bool {
	spanCtx, span := observability.StartSpan(context.Background(), "resource.optimize", "ResourceOptimizer.OptimizeIfNeeded")
	defer observability.FinishSpan(span, nil)

	ro.mu.RLock()
	lastOpt := ro.lastOptimization
	adaptive := ro.adaptiveMode
	ro.mu.RUnlock()

	elapsed := time.Since(lastOpt)

	// Check if adaptive optimization is needed regardless of interval
	if adaptive && ro.shouldOptimize() {
		ro.logger.Info("Adaptive optimization triggered due to performance changes")
		observability.AddBreadcrumb(spanCtx, "resource_optimizer", "Adaptive optimization triggered", sentry.LevelInfo)
		ro.calculateOptimalConcurrency(config)
		ro.mu.Lock()
		ro.lastOptimization = time.Now()
		ro.mu.Unlock()
		span.SetData("trigger", "adaptive")
		return true
	}

	// Respect the regular optimization interval
	if elapsed < ro.optimizationInterval {
		span.SetData("trigger", "skipped")
		return false
	}

	ro.logger.Info("Regular optimization triggered", "interval", ro.optimizationInterval)
	observability.AddBreadcrumbWithData(spanCtx, "resource_optimizer", "Regular optimization triggered", sentry.LevelInfo, map[string]interface{}{
		"interval": ro.optimizationInterval.String(),
	})
	ro.calculateOptimalConcurrency(config)
	ro.mu.Lock()
	ro.lastOptimization = time.Now()
	ro.mu.Unlock()
	span.SetData("trigger", "interval")
	return true
}

// shouldOptimize determines if adaptive optimization should be triggered
func (ro *ResourceOptimizer) shouldOptimize() bool {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	// Need at least 5 snapshots to make decisions
	if len(ro.performanceHistory) < 5 {
		return false
	}

	// Get recent snapshots (last 5)
	recentSnapshots := ro.performanceHistory[len(ro.performanceHistory)-5:]

	// Calculate averages
	var avgCPU, avgMemory, avgErrorRate, avgResponseTime float64
	for _, snapshot := range recentSnapshots {
		avgCPU += snapshot.CPUUsage
		avgMemory += snapshot.MemoryUsage
		avgErrorRate += snapshot.ErrorRate
		avgResponseTime += snapshot.ResponseTime
	}
	avgCPU /= float64(len(recentSnapshots))
	avgMemory /= float64(len(recentSnapshots))
	avgErrorRate /= float64(len(recentSnapshots))
	avgResponseTime /= float64(len(recentSnapshots))

	// Trigger optimization if:
	// 1. High resource usage (CPU > 85% or Memory > 90%)
	// 2. High error rate (> 5%)
	// 3. High response time (> 5000ms)
	// 4. Too many goroutines (> 1000)
	if avgCPU > 85.0 || avgMemory > 90.0 || avgErrorRate > 5.0 || avgResponseTime > 5000.0 {
		return true
	}

	// Check goroutine count
	if runtime.NumGoroutine() > 1000 {
		return true
	}

	return false
}

// GetPerformanceHistory returns recent performance history.
//
// Parameters:
//
//	limit: Number of snapshots to return.
//
// Returns:
//
//	[]PerformanceSnapshot: History.
func (ro *ResourceOptimizer) GetPerformanceHistory(limit int) []PerformanceSnapshot {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	if limit <= 0 || limit > len(ro.performanceHistory) {
		limit = len(ro.performanceHistory)
	}

	start := len(ro.performanceHistory) - limit
	return ro.performanceHistory[start:]
}

// GetSystemInfo returns current system information.
//
// Returns:
//
//	map[string]interface{}: System info.
func (ro *ResourceOptimizer) GetSystemInfo() map[string]interface{} {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	return map[string]interface{}{
		"cpu_cores":         ro.cpuCores,
		"memory_gb":         ro.memoryGB,
		"current_cpu":       ro.currentCPUUsage,
		"current_memory":    ro.currentMemoryUsage,
		"goroutines":        runtime.NumGoroutine(),
		"last_optimization": ro.lastOptimization,
		"adaptive_mode":     ro.adaptiveMode,
		"optimal_config":    ro.optimalConcurrency,
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
