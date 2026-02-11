package services

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/redis/go-redis/v9"
)

// PerformanceMonitor tracks system and application performance metrics.
type PerformanceMonitor struct {
	logger *zaplogrus.Logger
	redis  *redis.Client
	ctx    context.Context
	mu     sync.RWMutex

	// System metrics
	systemMetrics SystemMetrics

	// Application metrics
	appMetrics ApplicationMetrics

	// Collection intervals
	metricsInterval time.Duration
	stopChan        chan struct{}
}

// SystemMetrics holds system-level performance data.
type SystemMetrics struct {
	CPUUsage    float64   `json:"cpu_usage"`
	MemoryUsage uint64    `json:"memory_usage"`
	MemoryTotal uint64    `json:"memory_total"`
	Goroutines  int       `json:"goroutines"`
	GCPauses    []float64 `json:"gc_pauses"`
	LastUpdated time.Time `json:"last_updated"`
	HeapAlloc   uint64    `json:"heap_alloc"`
	HeapSys     uint64    `json:"heap_sys"`
	HeapInuse   uint64    `json:"heap_inuse"`
	StackInuse  uint64    `json:"stack_inuse"`
	NumGC       uint32    `json:"num_gc"`
}

// ApplicationMetrics holds application-specific performance data.
type ApplicationMetrics struct {
	// Collector metrics
	CollectorMetrics CollectorPerformanceMetrics `json:"collector"`

	// Redis metrics
	RedisMetrics RedisPerformanceMetrics `json:"redis"`

	// API metrics
	APIMetrics APIPerformanceMetrics `json:"api"`

	// Telegram metrics
	TelegramMetrics TelegramPerformanceMetrics `json:"telegram"`

	LastUpdated time.Time `json:"last_updated"`
}

// CollectorPerformanceMetrics tracks collector service performance.
type CollectorPerformanceMetrics struct {
	ActiveWorkers         int           `json:"active_workers"`
	TotalCollections      int64         `json:"total_collections"`
	SuccessfulCollections int64         `json:"successful_collections"`
	FailedCollections     int64         `json:"failed_collections"`
	AvgCollectionDuration time.Duration `json:"avg_collection_duration_ms"`
	CacheHits             int64         `json:"cache_hits"`
	CacheMisses           int64         `json:"cache_misses"`
	ConcurrentOperations  int           `json:"concurrent_operations"`
	WorkerPoolUtilization float64       `json:"worker_pool_utilization"`
	SymbolsFetched        int64         `json:"symbols_fetched"`
	BackfillOperations    int64         `json:"backfill_operations"`
}

// RedisPerformanceMetrics tracks Redis performance.
type RedisPerformanceMetrics struct {
	Connections       int           `json:"connections"`
	Hits              int64         `json:"hits"`
	Misses            int64         `json:"misses"`
	Evictions         int64         `json:"evictions"`
	MemoryUsage       int64         `json:"memory_usage"`
	AvgResponseTime   time.Duration `json:"avg_response_time_ms"`
	CommandsProcessed int64         `json:"commands_processed"`
	ErrorRate         float64       `json:"error_rate"`
}

// APIPerformanceMetrics tracks API performance.
type APIPerformanceMetrics struct {
	TotalRequests      int64         `json:"total_requests"`
	SuccessfulRequests int64         `json:"successful_requests"`
	FailedRequests     int64         `json:"failed_requests"`
	AvgResponseTime    time.Duration `json:"avg_response_time_ms"`
	ActiveConnections  int           `json:"active_connections"`
	RateLimitHits      int64         `json:"rate_limit_hits"`
}

// TelegramPerformanceMetrics tracks Telegram bot performance.
type TelegramPerformanceMetrics struct {
	MessagesProcessed int64         `json:"messages_processed"`
	NotificationsSent int64         `json:"notifications_sent"`
	WebhookRequests   int64         `json:"webhook_requests"`
	AvgProcessingTime time.Duration `json:"avg_processing_time_ms"`
	ActiveUsers       int           `json:"active_users"`
	RateLimitedUsers  int64         `json:"rate_limited_users"`
}

// NewPerformanceMonitor creates a new performance monitor.
//
// Parameters:
//
//	logger: Logger.
//	redis: Redis client.
//	ctx: Context.
//
// Returns:
//
//	*PerformanceMonitor: Initialized monitor.
func NewPerformanceMonitor(logger *zaplogrus.Logger, redis *redis.Client, ctx context.Context) *PerformanceMonitor {
	return &PerformanceMonitor{
		logger:          logger,
		redis:           redis,
		ctx:             ctx,
		metricsInterval: 30 * time.Second,
		stopChan:        make(chan struct{}),
	}
}

// Start begins performance monitoring.
func (pm *PerformanceMonitor) Start() {
	pm.logger.Info("Starting performance monitor")

	ticker := time.NewTicker(pm.metricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pm.collectMetrics()
		case <-pm.stopChan:
			pm.logger.Info("Performance monitor stopped")
			return
		case <-pm.ctx.Done():
			pm.logger.Info("Performance monitor context cancelled")
			return
		}
	}
}

// Stop stops performance monitoring.
func (pm *PerformanceMonitor) Stop() {
	select {
	case <-pm.stopChan:
		// Channel already closed
	default:
		close(pm.stopChan)
	}
}

// collectMetrics gathers all performance metrics
func (pm *PerformanceMonitor) collectMetrics() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Collect system metrics
	pm.collectSystemMetrics()

	// Collect application metrics
	pm.collectApplicationMetrics()

	// Cache metrics in Redis
	pm.cacheMetrics()

	// Log performance summary
	pm.logPerformanceSummary()
}

// collectSystemMetrics gathers system-level metrics
func (pm *PerformanceMonitor) collectSystemMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	pm.systemMetrics = SystemMetrics{
		MemoryUsage: m.Alloc,
		MemoryTotal: m.Sys,
		Goroutines:  runtime.NumGoroutine(),
		LastUpdated: time.Now(),
		HeapAlloc:   m.HeapAlloc,
		HeapSys:     m.HeapSys,
		HeapInuse:   m.HeapInuse,
		StackInuse:  m.StackInuse,
		NumGC:       m.NumGC,
	}

	// Calculate GC pause times
	if len(m.PauseNs) > 0 {
		pm.systemMetrics.GCPauses = make([]float64, len(m.PauseNs))
		for i, pause := range m.PauseNs {
			pm.systemMetrics.GCPauses[i] = float64(pause) / 1e6 // Convert to milliseconds
		}
	}
}

// collectApplicationMetrics gathers application-specific metrics
func (pm *PerformanceMonitor) collectApplicationMetrics() {
	pm.appMetrics.LastUpdated = time.Now()

	// Collect Redis metrics
	pm.collectRedisMetrics()
}

// collectRedisMetrics gathers Redis performance data
func (pm *PerformanceMonitor) collectRedisMetrics() {
	if pm.redis == nil {
		return
	}

	// Get Redis info
	_, err := pm.redis.Info(pm.ctx, "stats", "memory").Result()
	if err != nil {
		pm.logger.WithError(err).Warn("Failed to get Redis info")
		return
	}

	// Parse Redis info (simplified)
	pm.appMetrics.RedisMetrics = RedisPerformanceMetrics{
		Connections:       1, // Simplified
		CommandsProcessed: 0, // Would parse from info
		MemoryUsage:       0, // Would parse from info
	}
}

// cacheMetrics stores metrics in Redis for external access
func (pm *PerformanceMonitor) cacheMetrics() {
	if pm.redis == nil {
		return
	}

	// Cache system metrics
	systemData, err := json.Marshal(pm.systemMetrics)
	if err == nil {
		pm.redis.Set(pm.ctx, "performance:system", systemData, 5*time.Minute)
	}

	// Cache application metrics
	appData, err := json.Marshal(pm.appMetrics)
	if err == nil {
		pm.redis.Set(pm.ctx, "performance:application", appData, 5*time.Minute)
	}
}

// logPerformanceSummary logs a summary of current performance
func (pm *PerformanceMonitor) logPerformanceSummary() {
	pm.logger.WithFields(zaplogrus.Fields{
		"memory_mb":  pm.systemMetrics.MemoryUsage / 1024 / 1024,
		"goroutines": pm.systemMetrics.Goroutines,
		"heap_alloc": pm.systemMetrics.HeapAlloc / 1024 / 1024,
		"num_gc":     pm.systemMetrics.NumGC,
	}).Debug("Performance metrics collected")
}

// GetSystemMetrics returns current system metrics.
//
// Returns:
//
//	SystemMetrics: System metrics.
func (pm *PerformanceMonitor) GetSystemMetrics() SystemMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.systemMetrics
}

// GetApplicationMetrics returns current application metrics.
//
// Returns:
//
//	ApplicationMetrics: App metrics.
func (pm *PerformanceMonitor) GetApplicationMetrics() ApplicationMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.appMetrics
}

// UpdateCollectorMetrics updates collector performance metrics.
//
// Parameters:
//
//	metrics: New metrics.
func (pm *PerformanceMonitor) UpdateCollectorMetrics(metrics CollectorPerformanceMetrics) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.appMetrics.CollectorMetrics = metrics
}

// UpdateAPIMetrics updates API performance metrics.
//
// Parameters:
//
//	metrics: New metrics.
func (pm *PerformanceMonitor) UpdateAPIMetrics(metrics APIPerformanceMetrics) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.appMetrics.APIMetrics = metrics
}

// UpdateTelegramMetrics updates Telegram performance metrics.
//
// Parameters:
//
//	metrics: New metrics.
func (pm *PerformanceMonitor) UpdateTelegramMetrics(metrics TelegramPerformanceMetrics) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.appMetrics.TelegramMetrics = metrics
}

// RecordWorkerHealth records worker health status for monitoring.
//
// Parameters:
//
//	exchange: Exchange name.
//	isRunning: Running status.
//	errorCount: Error count.
func (pm *PerformanceMonitor) RecordWorkerHealth(exchange string, isRunning bool, errorCount int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Update collector metrics with worker health information
	if isRunning {
		pm.appMetrics.CollectorMetrics.ActiveWorkers++
	} else {
		if pm.appMetrics.CollectorMetrics.ActiveWorkers > 0 {
			pm.appMetrics.CollectorMetrics.ActiveWorkers--
		}
	}

	// Track failed collections based on error count
	pm.appMetrics.CollectorMetrics.FailedCollections += int64(errorCount)

	// Cache worker health in Redis for external monitoring
	if pm.redis != nil {
		workerHealth := map[string]interface{}{
			"exchange":    exchange,
			"is_running":  isRunning,
			"error_count": errorCount,
			"timestamp":   time.Now().Unix(),
		}

		if healthJSON, err := json.Marshal(workerHealth); err == nil {
			key := fmt.Sprintf("worker_health:%s", exchange)
			pm.redis.Set(pm.ctx, key, string(healthJSON), 10*time.Minute)
		}
	}

	// Log worker health status
	pm.logger.WithFields(zaplogrus.Fields{
		"exchange":    exchange,
		"is_running":  isRunning,
		"error_count": errorCount,
	}).Debug("Worker health recorded")
}

// GetPerformanceReport generates a comprehensive performance report.
//
// Returns:
//
//	map[string]interface{}: Report.
func (pm *PerformanceMonitor) GetPerformanceReport() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	report := map[string]interface{}{
		"system":       pm.systemMetrics,
		"application":  pm.appMetrics,
		"timestamp":    time.Now(),
		"health_score": pm.calculateHealthScore(),
	}

	return report
}

// Note: Duplicate RecordWorkerHealth method removed

// calculateHealthScore calculates an overall health score (0-100)
func (pm *PerformanceMonitor) calculateHealthScore() float64 {
	score := 100.0

	// Deduct points for high memory usage (>80% of heap)
	if pm.systemMetrics.HeapInuse > 0 && pm.systemMetrics.HeapSys > 0 {
		memoryUsagePercent := float64(pm.systemMetrics.HeapInuse) / float64(pm.systemMetrics.HeapSys) * 100
		if memoryUsagePercent > 80 {
			score -= (memoryUsagePercent - 80) * 2
		}
	}

	// Deduct points for too many goroutines (>1000)
	if pm.systemMetrics.Goroutines > 1000 {
		score -= float64(pm.systemMetrics.Goroutines-1000) * 0.01
	}

	// Deduct points for collector failures
	if pm.appMetrics.CollectorMetrics.TotalCollections > 0 {
		failureRate := float64(pm.appMetrics.CollectorMetrics.FailedCollections) / float64(pm.appMetrics.CollectorMetrics.TotalCollections) * 100
		if failureRate > 5 {
			score -= (failureRate - 5) * 2
		}
	}

	if score < 0 {
		score = 0
	}

	return score
}
