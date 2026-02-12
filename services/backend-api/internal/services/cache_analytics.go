package services

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/redis/go-redis/v9"
)

// CacheStats represents cache statistics.
type CacheStats struct {
	// Hits is the number of cache hits.
	Hits int64 `json:"hits"`
	// Misses is the number of cache misses.
	Misses int64 `json:"misses"`
	// HitRate is the ratio of hits to total operations.
	HitRate float64 `json:"hit_rate"`
	// TotalOps is the total number of cache operations.
	TotalOps int64 `json:"total_ops"`
	// LastUpdated is the time of the last update.
	LastUpdated time.Time `json:"last_updated"`
}

// CacheMetrics represents detailed cache metrics by category.
type CacheMetrics struct {
	// Overall contains aggregated stats.
	Overall CacheStats `json:"overall"`
	// ByCategory contains stats per category.
	ByCategory map[string]CacheStats `json:"by_category"`
	// RedisInfo contains raw Redis info.
	RedisInfo map[string]string `json:"redis_info"`
	// MemoryUsage is the memory used by Redis in bytes.
	MemoryUsage int64 `json:"memory_usage_bytes"`
	// ConnectedClients is the number of connected clients.
	ConnectedClients int64 `json:"connected_clients"`
	// KeyCount is the total number of keys.
	KeyCount int64 `json:"key_count"`
}

// CacheAnalyticsService tracks cache performance metrics.
type CacheAnalyticsService struct {
	redisClient *redis.Client
	stats       map[string]*CacheStats
	mu          sync.RWMutex
}

// NewCacheAnalyticsService creates a new cache analytics service.
//
// Parameters:
//
//	redisClient: Redis client.
//
// Returns:
//
//	*CacheAnalyticsService: Initialized service.
func NewCacheAnalyticsService(redisClient *redis.Client) *CacheAnalyticsService {
	return &CacheAnalyticsService{
		redisClient: redisClient,
		stats:       make(map[string]*CacheStats),
	}
}

// RecordHit records a cache hit for the given category.
//
// Parameters:
//
//	category: Cache category.
func (c *CacheAnalyticsService) RecordHit(category string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stats[category] == nil {
		c.stats[category] = &CacheStats{}
	}

	c.stats[category].Hits++
	c.stats[category].TotalOps++
	c.stats[category].HitRate = float64(c.stats[category].Hits) / float64(c.stats[category].TotalOps)
	c.stats[category].LastUpdated = time.Now()

	// Also update overall stats
	if c.stats["overall"] == nil {
		c.stats["overall"] = &CacheStats{}
	}
	c.stats["overall"].Hits++
	c.stats["overall"].TotalOps++
	c.stats["overall"].HitRate = float64(c.stats["overall"].Hits) / float64(c.stats["overall"].TotalOps)
	c.stats["overall"].LastUpdated = time.Now()
}

// RecordMiss records a cache miss for the given category.
//
// Parameters:
//
//	category: Cache category.
func (c *CacheAnalyticsService) RecordMiss(category string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stats[category] == nil {
		c.stats[category] = &CacheStats{}
	}

	c.stats[category].Misses++
	c.stats[category].TotalOps++
	c.stats[category].HitRate = float64(c.stats[category].Hits) / float64(c.stats[category].TotalOps)
	c.stats[category].LastUpdated = time.Now()

	// Also update overall stats
	if c.stats["overall"] == nil {
		c.stats["overall"] = &CacheStats{}
	}
	c.stats["overall"].Misses++
	c.stats["overall"].TotalOps++
	c.stats["overall"].HitRate = float64(c.stats["overall"].Hits) / float64(c.stats["overall"].TotalOps)
	c.stats["overall"].LastUpdated = time.Now()
}

// GetStats returns cache statistics for a specific category.
//
// Parameters:
//
//	category: Cache category.
//
// Returns:
//
//	CacheStats: Stats for the category.
func (c *CacheAnalyticsService) GetStats(category string) CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if stats, exists := c.stats[category]; exists {
		return *stats
	}
	return CacheStats{}
}

// GetAllStats returns all cache statistics.
//
// Returns:
//
//	map[string]CacheStats: Map of category to stats.
func (c *CacheAnalyticsService) GetAllStats() map[string]CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]CacheStats)
	for category, stats := range c.stats {
		result[category] = *stats
	}
	return result
}

// GetMetrics returns comprehensive cache metrics including Redis info.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	*CacheMetrics: Detailed metrics.
//	error: Error if retrieval fails.
func (c *CacheAnalyticsService) GetMetrics(ctx context.Context) (*CacheMetrics, error) {
	spanCtx, span := observability.StartSpan(ctx, "cache.metrics", "CacheAnalyticsService.GetMetrics")
	defer observability.FinishSpan(span, nil)

	c.mu.RLock()
	allStats := make(map[string]CacheStats)
	for category, stats := range c.stats {
		allStats[category] = *stats
	}
	c.mu.RUnlock()

	span.SetData("category_count", len(allStats))
	metrics := &CacheMetrics{
		ByCategory: allStats,
		RedisInfo:  make(map[string]string),
	}

	// Guard against nil Redis client
	if c.redisClient == nil {
		// Return metrics with default values when Redis is unavailable
		span.SetData("redis_available", false)
		observability.AddBreadcrumb(spanCtx, "cache_analytics", "Redis client unavailable, returning cached stats only", sentry.LevelWarning)
		if overall, exists := allStats["overall"]; exists {
			metrics.Overall = overall
		}
		return metrics, nil
	}
	span.SetData("redis_available", true)

	// Get Redis info
	redisInfo, err := c.redisClient.Info(spanCtx, "memory", "clients", "keyspace").Result()
	if err != nil {
		// Return metrics with available stats if Redis info fails
		span.SetData("redis_info_error", err.Error())
		observability.AddBreadcrumbWithData(spanCtx, "cache_analytics", "Failed to fetch Redis info", sentry.LevelWarning, map[string]interface{}{
			"error": err.Error(),
		})
		observability.CaptureExceptionWithContext(spanCtx, err, "redis_info", map[string]interface{}{
			"sections": []string{"memory", "clients", "keyspace"},
		})
		if overall, exists := allStats["overall"]; exists {
			metrics.Overall = overall
		}
		return metrics, nil
	}

	// Parse Redis info
	infoMap := c.parseRedisInfo(redisInfo)
	metrics.RedisInfo = infoMap

	// Get memory usage (ignore errors to provide partial metrics)
	memoryUsage, memErr := c.redisClient.MemoryUsage(spanCtx, "*").Result()
	if memErr != nil {
		observability.AddBreadcrumbWithData(spanCtx, "cache_analytics", "Failed to fetch Redis memory usage", sentry.LevelWarning, map[string]interface{}{
			"error": memErr.Error(),
		})
	} else {
		metrics.MemoryUsage = memoryUsage
	}

	// Get connected clients (ignore errors to provide partial metrics)
	clientList, clientErr := c.redisClient.ClientList(spanCtx).Result()
	if clientErr != nil {
		observability.AddBreadcrumbWithData(spanCtx, "cache_analytics", "Failed to fetch Redis client list", sentry.LevelWarning, map[string]interface{}{
			"error": clientErr.Error(),
		})
	} else {
		metrics.ConnectedClients = int64(len(clientList))
	}

	// Get key count (ignore errors to provide partial metrics)
	keyCount, keyErr := c.redisClient.DBSize(spanCtx).Result()
	if keyErr != nil {
		observability.AddBreadcrumbWithData(spanCtx, "cache_analytics", "Failed to fetch Redis key count", sentry.LevelWarning, map[string]interface{}{
			"error": keyErr.Error(),
		})
	} else {
		metrics.KeyCount = keyCount
	}

	// Set overall stats
	if overall, exists := allStats["overall"]; exists {
		metrics.Overall = overall
	}

	return metrics, nil
}

// parseRedisInfo parses Redis INFO command output
func (c *CacheAnalyticsService) parseRedisInfo(info string) map[string]string {
	result := make(map[string]string)

	if info == "" {
		return result
	}

	lines := strings.Split(info, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			result[key] = value
		}
	}

	return result
}

// ResetStats resets all cache statistics.
func (c *CacheAnalyticsService) ResetStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = make(map[string]*CacheStats)
}

// StartPeriodicReporting starts periodic reporting of cache stats to Redis.
//
// Parameters:
//
//	ctx: Context for cancellation.
//	interval: Reporting interval.
func (c *CacheAnalyticsService) StartPeriodicReporting(ctx context.Context, interval time.Duration) {
	observability.AddBreadcrumbWithData(ctx, "cache_analytics", "Starting cache analytics periodic reporting", sentry.LevelInfo, map[string]interface{}{
		"interval": interval.String(),
	})
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		defer observability.RecoverAndCapture(ctx, "CacheAnalyticsService.StartPeriodicReporting")
		for {
			select {
			case <-ctx.Done():
				observability.AddBreadcrumb(ctx, "cache_analytics", "Stopping cache analytics periodic reporting", sentry.LevelInfo)
				return
			case <-ticker.C:
				c.reportStats(ctx)
			}
		}
	}()
}

// reportStats reports current stats to Redis for persistence
func (c *CacheAnalyticsService) reportStats(ctx context.Context) {
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpCacheSet, "CacheAnalyticsService.reportStats")
	defer observability.FinishSpan(span, nil)

	// Guard against nil Redis client
	if c.redisClient == nil {
		observability.AddBreadcrumb(spanCtx, "cache_analytics", "Skipping cache analytics report; Redis client unavailable", sentry.LevelWarning)
		return
	}

	allStats := c.GetAllStats()
	statsJSON, err := json.Marshal(allStats)
	if err != nil {
		observability.CaptureExceptionWithContext(spanCtx, err, "cache_analytics.marshal", map[string]interface{}{
			"category_count": len(allStats),
		})
		return
	}

	// Store stats in Redis with 24 hour TTL (ignore errors)
	if setErr := c.redisClient.Set(spanCtx, "cache:analytics:stats", statsJSON, 24*time.Hour).Err(); setErr != nil {
		observability.CaptureExceptionWithContext(spanCtx, setErr, "cache_analytics.store", map[string]interface{}{
			"key": "cache:analytics:stats",
		})
	}
}
