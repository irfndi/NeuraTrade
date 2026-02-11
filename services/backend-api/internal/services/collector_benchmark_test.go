package services

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/testutil"
)

// getRedisAddr returns the Redis address from environment or default
func getRedisAddr() string {
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379" // fallback for local development
}

// BenchmarkCollectorService_ConcurrentSymbolFetching benchmarks the concurrent symbol fetching optimization
func BenchmarkCollectorService_ConcurrentSymbolFetching(b *testing.B) {
	ctx := context.Background()
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel) // Reduce noise during benchmarks

	// Create mock Redis client
	options := testutil.GetTestRedisOptions()
	options.DB = 1 // Use test database
	rdb := redis.NewClient(options)

	// Create test config
	cfg := &config.Config{}
	collectorCfg := CollectorConfig{
		IntervalSeconds: 60,
		MaxErrors:       10,
	}

	// Create collector service
	service := &CollectorService{
		config:          cfg,
		collectorConfig: collectorCfg,
		redisClient:     rdb,
		ctx:             ctx,
		workers:         make(map[string]*Worker),
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Benchmark concurrent symbol fetching
			exchanges := []string{"binance", "okx", "bybit"}
			symbols, err := service.getMultiExchangeSymbols(exchanges)
			if err != nil {
				b.Errorf("Error fetching symbols: %v", err)
			}
			_ = symbols
		}
	})
}

// BenchmarkCollectorService_BulkTickerCollection benchmarks the bulk ticker collection optimization
func BenchmarkCollectorService_BulkTickerCollection(b *testing.B) {
	ctx := context.Background()
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)

	options := testutil.GetTestRedisOptions()
	options.DB = 1
	rdb := redis.NewClient(options)

	cfg := &config.Config{}
	collectorCfg := CollectorConfig{
		IntervalSeconds: 60,
		MaxErrors:       5,
	}

	service := &CollectorService{
		config:          cfg,
		collectorConfig: collectorCfg,
		redisClient:     rdb,
		ctx:             ctx,
		workers:         make(map[string]*Worker),
	}

	// Create test worker
	testWorker := &Worker{
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT", "ETH/USDT", "BNB/USDT", "ADA/USDT", "DOT/USDT"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Benchmark bulk ticker collection
		err := service.collectTickerDataBulk(testWorker)
		if err != nil {
			b.Errorf("Error collecting bulk ticker data: %v", err)
		}
	}
}

// BenchmarkCollectorService_ConcurrentBackfill benchmarks the concurrent backfill optimization
func BenchmarkCollectorService_ConcurrentBackfill(b *testing.B) {
	ctx := context.Background()
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)

	options := testutil.GetTestRedisOptions()
	options.DB = 1
	rdb := redis.NewClient(options)

	cfg := &config.Config{}
	collectorCfg := CollectorConfig{
		IntervalSeconds: 60,
		MaxErrors:       5,
	}

	service := &CollectorService{
		config:          cfg,
		collectorConfig: collectorCfg,
		redisClient:     rdb,
		ctx:             ctx,
		workers:         make(map[string]*Worker),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Benchmark concurrent backfill
		err := service.performHistoricalBackfillConcurrent()
		if err != nil {
			b.Errorf("Error performing concurrent backfill: %v", err)
		}
	}
}

// BenchmarkCollectorService_RedisOperations benchmarks Redis caching operations
func BenchmarkCollectorService_RedisOperations(b *testing.B) {
	options := testutil.GetTestRedisOptions()
	options.DB = 1
	rdb := redis.NewClient(options)

	ctx := context.Background()
	testData := map[string]interface{}{
		"symbol":    "BTC/USDT",
		"price":     50000.0,
		"volume":    1000.0,
		"timestamp": time.Now().Unix(),
	}

	b.Run("Redis Set", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cacheKey := "benchmark:ticker:BTC/USDT"
			err := rdb.Set(ctx, cacheKey, testData, 10*time.Second).Err()
			if err != nil {
				b.Errorf("Redis set error: %v", err)
			}
		}
	})

	b.Run("Redis Get", func(b *testing.B) {
		// Pre-populate cache
		cacheKey := "benchmark:ticker:BTC/USDT"
		rdb.Set(ctx, cacheKey, testData, 10*time.Second)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := rdb.Get(ctx, cacheKey).Result()
			if err != nil && err != redis.Nil {
				b.Errorf("Redis get error: %v", err)
			}
		}
	})

	b.Run("Redis Pipeline", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pipe := rdb.Pipeline()
			for j := 0; j < 10; j++ {
				cacheKey := fmt.Sprintf("benchmark:pipeline:%d", j)
				pipe.Set(ctx, cacheKey, testData, 10*time.Second)
			}
			_, err := pipe.Exec(ctx)
			if err != nil {
				b.Errorf("Redis pipeline error: %v", err)
			}
		}
	})
}

// BenchmarkCollectorService_WorkerPoolPerformance benchmarks worker pool efficiency
func BenchmarkCollectorService_WorkerPoolPerformance(b *testing.B) {
	ctx := context.Background()
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)

	cfg := &config.Config{}
	collectorCfg := CollectorConfig{
		IntervalSeconds: 60,
		MaxErrors:       10,
	}

	service := &CollectorService{
		config:          cfg,
		collectorConfig: collectorCfg,
		ctx:             ctx,
		workers:         make(map[string]*Worker),
	}

	b.Run("Worker Creation", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			multiExchangeSymbols := map[string]int{"BTC/USDT": 2}
			err := service.createWorker("binance", multiExchangeSymbols)
			if err != nil {
				b.Errorf("Error creating worker: %v", err)
			}
		}
	})

	b.Run("Worker Pool Initialization", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate worker pool initialization
			exchanges := []string{"binance", "okx", "bybit"}
			multiExchangeSymbols := map[string]int{"BTC/USDT": 2}
			for _, exchange := range exchanges {
				err := service.createWorker(exchange, multiExchangeSymbols)
				if err != nil {
					b.Errorf("Error creating worker for %s: %v", exchange, err)
				}
			}
		}
	})
}

// Performance monitoring helper functions
func (c *CollectorService) GetPerformanceMetrics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := make(map[string]interface{})
	metrics["active_workers"] = len(c.workers)
	// Note: stats field doesn't exist in actual CollectorService
	// These would need to be tracked separately or through performance monitor
	metrics["total_collections"] = 0
	metrics["successful_collections"] = 0
	metrics["failed_collections"] = 0
	metrics["cache_hits"] = 0
	metrics["cache_misses"] = 0
	metrics["avg_collection_duration_ms"] = 0

	return metrics
}

func (c *CollectorService) ResetPerformanceMetrics() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Note: stats field doesn't exist in actual CollectorService
	// This would need to be implemented differently
}

// Memory usage benchmark
func BenchmarkCollectorService_MemoryUsage(b *testing.B) {
	ctx := context.Background()
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)

	rdb := redis.NewClient(&redis.Options{
		Addr: getRedisAddr(),
		DB:   1,
	})

	cfg := &config.Config{}
	collectorCfg := CollectorConfig{
		IntervalSeconds: 60,
		MaxErrors:       5,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		service := &CollectorService{
			config:          cfg,
			collectorConfig: collectorCfg,
			redisClient:     rdb,
			ctx:             ctx,
			workers:         make(map[string]*Worker),
		}

		// Simulate typical operations
		service.initializeWorkersAsync()
		_ = service
	}
}
