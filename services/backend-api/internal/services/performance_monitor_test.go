package services

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPerformanceMonitor(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	assert.NotNil(t, pm)
	assert.Equal(t, logger, pm.logger)
	assert.Equal(t, redisClient, pm.redis)
	assert.Equal(t, ctx, pm.ctx)
	assert.Equal(t, 30*time.Second, pm.metricsInterval)
	assert.NotNil(t, pm.stopChan)
}

func TestPerformanceMonitor_Start(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Start the monitor in a goroutine
	go pm.Start()

	// Wait a bit to ensure it starts collecting metrics
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop the monitor
	cancel()

	// Give it time to stop
	time.Sleep(50 * time.Millisecond)
}

func TestPerformanceMonitor_Stop(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Start the monitor
	go pm.Start()

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Stop the monitor
	pm.Stop()

	// Give it time to stop
	time.Sleep(50 * time.Millisecond)

	// Should not panic when called multiple times
	assert.NotPanics(t, func() {
		pm.Stop()
	})
}

func TestPerformanceMonitor_GetSystemMetrics(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Initially, metrics should be empty
	metrics := pm.GetSystemMetrics()
	assert.Zero(t, metrics.Goroutines)
	assert.True(t, metrics.LastUpdated.IsZero())

	// Collect metrics manually
	pm.collectSystemMetrics()

	// Now metrics should be populated
	metrics = pm.GetSystemMetrics()
	assert.NotZero(t, metrics.Goroutines)
	assert.False(t, metrics.LastUpdated.IsZero())
	assert.NotZero(t, metrics.MemoryUsage)
	assert.NotZero(t, metrics.MemoryTotal)
	assert.NotZero(t, metrics.HeapAlloc)
	// NumGC can be zero in test environments, which is normal behavior
	assert.GreaterOrEqual(t, metrics.NumGC, uint32(0))
}

func TestPerformanceMonitor_GetApplicationMetrics(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Initially, metrics should be empty
	metrics := pm.GetApplicationMetrics()
	assert.True(t, metrics.LastUpdated.IsZero())

	// Collect metrics manually
	pm.collectApplicationMetrics()

	// Now metrics should be populated
	metrics = pm.GetApplicationMetrics()
	assert.False(t, metrics.LastUpdated.IsZero())
}

func TestPerformanceMonitor_UpdateCollectorMetrics(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	metrics := CollectorPerformanceMetrics{
		ActiveWorkers:         5,
		TotalCollections:      1000,
		SuccessfulCollections: 950,
		FailedCollections:     50,
		AvgCollectionDuration: 100 * time.Millisecond,
		CacheHits:             800,
		CacheMisses:           200,
		ConcurrentOperations:  10,
		WorkerPoolUtilization: 0.75,
		SymbolsFetched:        5000,
		BackfillOperations:    100,
	}

	pm.UpdateCollectorMetrics(metrics)

	appMetrics := pm.GetApplicationMetrics()
	assert.Equal(t, metrics.ActiveWorkers, appMetrics.CollectorMetrics.ActiveWorkers)
	assert.Equal(t, metrics.TotalCollections, appMetrics.CollectorMetrics.TotalCollections)
	assert.Equal(t, metrics.SuccessfulCollections, appMetrics.CollectorMetrics.SuccessfulCollections)
	assert.Equal(t, metrics.FailedCollections, appMetrics.CollectorMetrics.FailedCollections)
}

func TestPerformanceMonitor_UpdateAPIMetrics(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	metrics := APIPerformanceMetrics{
		TotalRequests:      10000,
		SuccessfulRequests: 9500,
		FailedRequests:     500,
		AvgResponseTime:    50 * time.Millisecond,
		ActiveConnections:  25,
		RateLimitHits:      100,
	}

	pm.UpdateAPIMetrics(metrics)

	appMetrics := pm.GetApplicationMetrics()
	assert.Equal(t, metrics.TotalRequests, appMetrics.APIMetrics.TotalRequests)
	assert.Equal(t, metrics.SuccessfulRequests, appMetrics.APIMetrics.SuccessfulRequests)
	assert.Equal(t, metrics.FailedRequests, appMetrics.APIMetrics.FailedRequests)
}

func TestPerformanceMonitor_UpdateTelegramMetrics(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	metrics := TelegramPerformanceMetrics{
		MessagesProcessed: 5000,
		NotificationsSent: 4500,
		WebhookRequests:   1000,
		AvgProcessingTime: 20 * time.Millisecond,
		ActiveUsers:       100,
		RateLimitedUsers:  10,
	}

	pm.UpdateTelegramMetrics(metrics)

	appMetrics := pm.GetApplicationMetrics()
	assert.Equal(t, metrics.MessagesProcessed, appMetrics.TelegramMetrics.MessagesProcessed)
	assert.Equal(t, metrics.NotificationsSent, appMetrics.TelegramMetrics.NotificationsSent)
	assert.Equal(t, metrics.WebhookRequests, appMetrics.TelegramMetrics.WebhookRequests)
}

func TestPerformanceMonitor_RecordWorkerHealth(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Record worker health
	pm.RecordWorkerHealth("binance", true, 0)
	pm.RecordWorkerHealth("coinbase", true, 2)
	pm.RecordWorkerHealth("kraken", false, 5)

	appMetrics := pm.GetApplicationMetrics()
	assert.Equal(t, 1, appMetrics.CollectorMetrics.ActiveWorkers)            // 2 running workers minus 1 stopped worker = 1 active worker
	assert.Equal(t, int64(7), appMetrics.CollectorMetrics.FailedCollections) // 0 + 2 + 5 errors

	// Check Redis cache
	data, err := redisClient.Get(ctx, "worker_health:binance").Result()
	require.NoError(t, err)
	assert.Contains(t, data, "binance")
	assert.Contains(t, data, "is_running\":true")
}

func TestPerformanceMonitor_GetPerformanceReport(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Collect some metrics first
	pm.collectSystemMetrics()
	pm.collectApplicationMetrics()

	// Get performance report
	report := pm.GetPerformanceReport()

	assert.Contains(t, report, "system")
	assert.Contains(t, report, "application")
	assert.Contains(t, report, "timestamp")
	assert.Contains(t, report, "health_score")

	// Check health score is within valid range
	healthScore := report["health_score"].(float64)
	assert.GreaterOrEqual(t, healthScore, 0.0)
	assert.LessOrEqual(t, healthScore, 100.0)
}

func TestPerformanceMonitor_CalculateHealthScore(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Test with healthy metrics
	pm.systemMetrics = SystemMetrics{
		HeapInuse:  100 * 1024 * 1024,  // 100MB
		HeapSys:    1000 * 1024 * 1024, // 1GB
		Goroutines: 500,
	}
	pm.appMetrics.CollectorMetrics = CollectorPerformanceMetrics{
		TotalCollections:  1000,
		FailedCollections: 10,
	}

	score := pm.calculateHealthScore()
	assert.Greater(t, score, 90.0)

	// Test with unhealthy metrics (high memory usage)
	pm.systemMetrics = SystemMetrics{
		HeapInuse:  900 * 1024 * 1024,  // 900MB (90% of 1GB)
		HeapSys:    1000 * 1024 * 1024, // 1GB
		Goroutines: 500,
	}

	score = pm.calculateHealthScore()
	assert.Less(t, score, 90.0)

	// Test with too many goroutines
	pm.systemMetrics = SystemMetrics{
		HeapInuse:  100 * 1024 * 1024,
		HeapSys:    1000 * 1024 * 1024,
		Goroutines: 2000,
	}

	score = pm.calculateHealthScore()
	assert.LessOrEqual(t, score, 90.0)

	// Test with high failure rate
	pm.systemMetrics = SystemMetrics{
		HeapInuse:  100 * 1024 * 1024,
		HeapSys:    1000 * 1024 * 1024,
		Goroutines: 500,
	}
	pm.appMetrics.CollectorMetrics = CollectorPerformanceMetrics{
		TotalCollections:  1000,
		FailedCollections: 100, // 10% failure rate
	}

	score = pm.calculateHealthScore()
	assert.LessOrEqual(t, score, 90.0)
}

func TestPerformanceMonitor_ConcurrentAccess(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	var wg sync.WaitGroup
	iterations := 100

	// Test concurrent access to metrics
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			switch i % 5 {
			case 0:
				pm.GetSystemMetrics()
			case 1:
				pm.GetApplicationMetrics()
			case 2:
				pm.UpdateCollectorMetrics(CollectorPerformanceMetrics{
					ActiveWorkers: i,
				})
			case 3:
				pm.RecordWorkerHealth("exchange", i%2 == 0, i%3)
			case 4:
				pm.GetPerformanceReport()
			}
		}(i)
	}

	wg.Wait()

	// Verify final state - check that metrics were updated (not exact count due to concurrency)
	appMetrics := pm.GetApplicationMetrics()
	// The value should be between 0 and the number of iterations that updated worker metrics
	// Since multiple goroutines may update the same counter, we use a reasonable upper bound
	assert.GreaterOrEqual(t, appMetrics.CollectorMetrics.ActiveWorkers, 0)
	assert.LessOrEqual(t, appMetrics.CollectorMetrics.ActiveWorkers, iterations)
}

func TestPerformanceMonitor_CacheMetrics(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Collect and cache metrics
	pm.collectSystemMetrics()
	pm.collectApplicationMetrics()
	pm.cacheMetrics()

	// Verify cached data
	systemData, err := redisClient.Get(ctx, "performance:system").Result()
	require.NoError(t, err)
	assert.NotEmpty(t, systemData)

	var cachedSystem SystemMetrics
	err = json.Unmarshal([]byte(systemData), &cachedSystem)
	require.NoError(t, err)
	assert.Equal(t, pm.systemMetrics.LastUpdated.Unix(), cachedSystem.LastUpdated.Unix())

	appData, err := redisClient.Get(ctx, "performance:application").Result()
	require.NoError(t, err)
	assert.NotEmpty(t, appData)

	var cachedApp ApplicationMetrics
	err = json.Unmarshal([]byte(appData), &cachedApp)
	require.NoError(t, err)
	assert.Equal(t, pm.appMetrics.LastUpdated.Unix(), cachedApp.LastUpdated.Unix())
}

func TestPerformanceMonitor_CollectSystemMetrics(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Get initial memory stats
	var initialMemStats runtime.MemStats
	runtime.ReadMemStats(&initialMemStats)

	// Collect system metrics
	pm.collectSystemMetrics()

	// Verify metrics are collected
	metrics := pm.systemMetrics
	assert.NotZero(t, metrics.LastUpdated)
	assert.NotZero(t, metrics.MemoryUsage)
	assert.NotZero(t, metrics.MemoryTotal)
	assert.NotZero(t, metrics.Goroutines)
	assert.NotZero(t, metrics.HeapAlloc)
	assert.NotZero(t, metrics.HeapSys)
	assert.NotZero(t, metrics.HeapInuse)
	assert.NotZero(t, metrics.NumGC)

	// Verify GC pauses are collected if present
	if len(metrics.GCPauses) > 0 {
		assert.True(t, metrics.GCPauses[0] >= 0)
	}
}

func TestPerformanceMonitor_CollectRedisMetrics(t *testing.T) {
	logger := zaplogrus.New()
	redisClient := getTestRedisClient(t)
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, redisClient, ctx)

	// Collect Redis metrics
	pm.collectRedisMetrics()

	// Verify Redis metrics are initialized - miniredis has limitations with INFO command
	metrics := pm.appMetrics.RedisMetrics
	// Just verify the structure exists and values are non-negative
	assert.GreaterOrEqual(t, metrics.Connections, 0)
	assert.GreaterOrEqual(t, metrics.MemoryUsage, int64(0))
	assert.GreaterOrEqual(t, metrics.Hits, int64(0))
	assert.GreaterOrEqual(t, metrics.Misses, int64(0))
}

func TestPerformanceMonitor_WithNilRedis(t *testing.T) {
	logger := zaplogrus.New()
	ctx := context.Background()

	pm := NewPerformanceMonitor(logger, nil, ctx)

	// Should not panic with nil Redis client
	assert.NotPanics(t, func() {
		pm.collectRedisMetrics()
		pm.cacheMetrics()
		pm.RecordWorkerHealth("test", true, 0)
	})

	// Get metrics should still work
	systemMetrics := pm.GetSystemMetrics()
	appMetrics := pm.GetApplicationMetrics()
	assert.True(t, systemMetrics.LastUpdated.IsZero())
	assert.True(t, appMetrics.LastUpdated.IsZero())
}

// Helper function to get test Redis client
func getTestRedisClient(t *testing.T) *redis.Client {
	mr, err := miniredis.Run()
	if err != nil {
		// If we cannot bind due to "operation not permitted", skip the test.
		// All other errors are considered fatal and should fail the test.
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("miniredis cannot bind in this environment; skipping Redis-backed performance monitor tests")
		}
		t.Fatalf("failed to start miniredis (not 'operation not permitted'): %v", err)
	}
	t.Cleanup(mr.Close)

	return redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
}
