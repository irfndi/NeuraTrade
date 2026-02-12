package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/irfndi/neuratrade/internal/api/handlers/testmocks"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/irfndi/neuratrade/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func TestCacheIntegrationProblem(t *testing.T) {
	// Setup
	mockCCXT := &testmocks.MockCCXTService{}
	mockRedisClient := testutil.GetTestRedisClient()
	mockRedis := &database.RedisClient{Client: mockRedisClient}
	cacheAnalytics := services.NewCacheAnalyticsService(mockRedisClient)

	// Create MarketHandler with CacheAnalyticsService
	marketHandler := NewMarketHandler(nil, mockCCXT, nil, mockRedis, cacheAnalytics)

	// Simulate a cache miss by calling a method that would increment cache stats
	ctx := context.Background()
	cacheKey := "test:ticker:binance:BTCUSDT"

	// This will now use CacheAnalyticsService to record the miss
	_, found := marketHandler.GetCachedTicker(ctx, cacheKey)
	assert.False(t, found, "Cache should miss for non-existent key")

	// Check CacheAnalyticsService stats (should now show the miss)
	stats := cacheAnalytics.GetStats("tickers")
	assert.Equal(t, int64(0), stats.Hits, "CacheAnalyticsService hits should be 0")
	assert.Equal(t, int64(1), stats.Misses, "CacheAnalyticsService misses should be 1 - now properly recorded!")

	// This test now demonstrates that MarketHandler's cache operations ARE being recorded
	// by CacheAnalyticsService, which should fix the /api/cache/metrics endpoint
	t.Logf("CacheAnalyticsService stats - Hits: %d, Misses: %d", stats.Hits, stats.Misses)
}

// TestCacheAnalyticsIntegration tests comprehensive cache analytics integration
func TestCacheAnalyticsIntegration(t *testing.T) {
	// Setup
	mockCCXT := &testmocks.MockCCXTService{}
	mockRedisClient := testutil.GetTestRedisClient()
	mockRedis := &database.RedisClient{Client: mockRedisClient}
	cacheAnalytics := services.NewCacheAnalyticsService(mockRedisClient)

	// Create MarketHandler with CacheAnalyticsService
	marketHandler := NewMarketHandler(nil, mockCCXT, nil, mockRedis, cacheAnalytics)
	ctx := context.Background()

	// Test different cache operations and categories
	t.Run("Ticker Cache Operations", func(t *testing.T) {
		// Test ticker cache miss
		_, found := marketHandler.GetCachedTicker(ctx, "test:ticker:binance:BTCUSDT")
		assert.False(t, found)

		// Check ticker stats
		tickerStats := cacheAnalytics.GetStats("tickers")
		assert.Equal(t, int64(0), tickerStats.Hits)
		assert.Equal(t, int64(1), tickerStats.Misses)
	})

	t.Run("Bulk Tickers Cache Operations", func(t *testing.T) {
		// Test bulk tickers cache miss
		_, found := marketHandler.GetCachedBulkTickers(ctx, "test:bulk_tickers:binance")
		assert.False(t, found)

		// Check bulk tickers stats
		bulkStats := cacheAnalytics.GetStats("bulk_tickers")
		assert.Equal(t, int64(0), bulkStats.Hits)
		assert.Equal(t, int64(1), bulkStats.Misses)
	})

	t.Run("Order Book Cache Operations", func(t *testing.T) {
		// Test order book cache miss
		_, found := marketHandler.GetCachedOrderBook(ctx, "test:orderbook:binance:BTCUSDT")
		assert.False(t, found)

		// Check order book stats
		orderBookStats := cacheAnalytics.GetStats("order_books")
		assert.Equal(t, int64(0), orderBookStats.Hits)
		assert.Equal(t, int64(1), orderBookStats.Misses)
	})

	t.Run("Market Prices Cache Operations", func(t *testing.T) {
		// Test market prices cache miss
		_, found := marketHandler.GetCachedMarketPrices(ctx, "test:market_prices:binance")
		assert.False(t, found)

		// Check market prices stats
		marketPricesStats := cacheAnalytics.GetStats("market_data")
		assert.Equal(t, int64(0), marketPricesStats.Hits)
		assert.Equal(t, int64(1), marketPricesStats.Misses)
	})

	t.Run("Overall Stats Aggregation", func(t *testing.T) {
		// Check overall stats (should aggregate all categories)
		overallStats := cacheAnalytics.GetStats("overall")
		assert.Equal(t, int64(0), overallStats.Hits)
		assert.Equal(t, int64(4), overallStats.Misses) // 4 misses from all categories

		t.Logf("Overall cache stats - Hits: %d, Misses: %d", overallStats.Hits, overallStats.Misses)
	})
}

// TestCacheAnalyticsCategories tests that different cache categories are properly tracked
func TestCacheAnalyticsCategories(t *testing.T) {
	mockRedisClient := testutil.GetTestRedisClient()
	cacheAnalytics := services.NewCacheAnalyticsService(mockRedisClient)

	// Test different categories
	categories := []string{"tickers", "bulk_tickers", "order_books", "market_data"}

	for _, category := range categories {
		t.Run(fmt.Sprintf("Category_%s", category), func(t *testing.T) {
			// Record operations for this category
			cacheAnalytics.RecordMiss(category)
			cacheAnalytics.RecordHit(category)
			cacheAnalytics.RecordHit(category)

			// Check category-specific stats
			stats := cacheAnalytics.GetStats(category)
			assert.Equal(t, int64(2), stats.Hits, fmt.Sprintf("%s should have 2 hits", category))
			assert.Equal(t, int64(1), stats.Misses, fmt.Sprintf("%s should have 1 miss", category))
			assert.Equal(t, int64(3), stats.TotalOps, fmt.Sprintf("%s should have 3 total ops", category))

			// Check hit rate calculation (should be decimal, not percentage)
			expectedHitRate := float64(2) / float64(3) // 0.6666...
			assert.InDelta(t, expectedHitRate, stats.HitRate, 0.01, fmt.Sprintf("%s hit rate should be ~0.67", category))
		})
	}
}
