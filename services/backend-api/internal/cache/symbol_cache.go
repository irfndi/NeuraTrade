package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/redis/go-redis/v9"
)

// SymbolCacheEntry represents a cached symbol entry with metadata.
type SymbolCacheEntry struct {
	// Symbols is the list of trading pair symbols.
	Symbols []string `json:"symbols"`
	// CachedAt is the timestamp when the data was cached.
	CachedAt time.Time `json:"cached_at"`
	// ExpiresAt is the timestamp when the cache entry expires.
	ExpiresAt time.Time `json:"expires_at"`
}

// SymbolCacheStats tracks cache performance metrics.
type SymbolCacheStats struct {
	// Hits is the number of successful cache lookups.
	Hits int64 `json:"hits"`
	// Misses is the number of failed cache lookups.
	Misses int64 `json:"misses"`
	// Sets is the number of times data was written to the cache.
	Sets int64 `json:"sets"`
	mu   sync.RWMutex
}

// RedisSymbolCache implements symbol caching using Redis.
type RedisSymbolCache struct {
	redis  *redis.Client
	ttl    time.Duration
	stats  *SymbolCacheStats
	prefix string
}

// NewRedisSymbolCache creates a new Redis-based symbol cache.
//
// Parameters:
//
//	redisClient: The Redis client.
//	ttl: The time-to-live for cache entries.
//
// Returns:
//
//	*RedisSymbolCache: The initialized cache.
func NewRedisSymbolCache(redisClient *redis.Client, ttl time.Duration) *RedisSymbolCache {
	return &RedisSymbolCache{
		redis:  redisClient,
		ttl:    ttl,
		stats:  &SymbolCacheStats{},
		prefix: "symbol_cache:",
	}
}

// Get retrieves symbols for an exchange from Redis cache.
//
// Parameters:
//
//	exchangeID: The exchange identifier.
//
// Returns:
//
//	[]string: The list of symbols.
//	bool: True if found and valid.
func (c *RedisSymbolCache) Get(exchangeID string) ([]string, bool) {
	ctx := context.Background()
	cacheKey := c.prefix + exchangeID

	// Get cached data from Redis
	data, err := c.redis.Get(ctx, cacheKey).Result()
	if err == redis.Nil {
		// Cache miss
		c.stats.mu.Lock()
		c.stats.Misses++
		c.stats.mu.Unlock()
		return nil, false
	}
	if err != nil {
		zaplogrus.Infof("Redis error getting symbols for %s: %v", exchangeID, err)
		c.stats.mu.Lock()
		c.stats.Misses++
		c.stats.mu.Unlock()
		return nil, false
	}

	// Deserialize cached entry
	var entry SymbolCacheEntry
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		zaplogrus.Infof("Error deserializing cached symbols for %s: %v", exchangeID, err)
		c.stats.mu.Lock()
		c.stats.Misses++
		c.stats.mu.Unlock()
		return nil, false
	}

	// Check if entry has expired (additional check beyond Redis TTL)
	if time.Now().After(entry.ExpiresAt) {
		// Entry expired, but return it anyway to prevent API calls during runtime
		// This matches the behavior of the original in-memory cache
		zaplogrus.Infof("Cached symbols for %s expired but returning to prevent API calls", exchangeID)
	}

	// Cache hit
	c.stats.mu.Lock()
	c.stats.Hits++
	c.stats.mu.Unlock()

	return entry.Symbols, true
}

// Set stores symbols for an exchange in Redis cache.
//
// Parameters:
//
//	exchangeID: The exchange identifier.
//	symbols: The list of symbols to cache.
func (c *RedisSymbolCache) Set(exchangeID string, symbols []string) {
	ctx := context.Background()
	cacheKey := c.prefix + exchangeID

	// Create cache entry with metadata
	now := time.Now()
	entry := SymbolCacheEntry{
		Symbols:   symbols,
		CachedAt:  now,
		ExpiresAt: now.Add(c.ttl),
	}

	// Serialize entry
	data, err := json.Marshal(entry)
	if err != nil {
		zaplogrus.Infof("Error serializing symbols for %s: %v", exchangeID, err)
		return
	}

	// Store in Redis with TTL
	err = c.redis.Set(ctx, cacheKey, data, c.ttl).Err()
	if err != nil {
		zaplogrus.Infof("Redis error setting symbols for %s: %v", exchangeID, err)
		return
	}

	// Update stats
	c.stats.mu.Lock()
	c.stats.Sets++
	c.stats.mu.Unlock()

	zaplogrus.Infof("Cached %d symbols for %s (TTL: %v)", len(symbols), exchangeID, c.ttl)
}

// GetStats returns current cache statistics.
//
// Returns:
//
//	SymbolCacheStats: The statistics.
func (c *RedisSymbolCache) GetStats() SymbolCacheStats {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()
	return SymbolCacheStats{
		Hits:   c.stats.Hits,
		Misses: c.stats.Misses,
		Sets:   c.stats.Sets,
	}
}

// LogStats logs current cache performance statistics.
func (c *RedisSymbolCache) LogStats() {
	stats := c.GetStats()
	total := stats.Hits + stats.Misses
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(stats.Hits) / float64(total) * 100
	}

	zaplogrus.Infof("Redis Symbol Cache Stats - Hits: %d, Misses: %d, Sets: %d, Hit Rate: %.2f%%",
		stats.Hits, stats.Misses, stats.Sets, hitRate)
}

// Clear removes all cached symbols (useful for testing or cache invalidation).
//
// Returns:
//
//	error: An error if clearing fails.
func (c *RedisSymbolCache) Clear() error {
	ctx := context.Background()
	pattern := c.prefix + "*"

	// Get all keys matching the pattern using SCAN for better performance
	var keys []string
	iter := c.redis.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("error scanning cache keys: %w", err)
	}

	if len(keys) == 0 {
		return nil
	}

	// Delete all matching keys
	if err := c.redis.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("error clearing cache: %w", err)
	}

	zaplogrus.Infof("Cleared %d symbol cache entries", len(keys))
	return nil
}

// GetCachedExchanges returns a list of exchanges that have cached symbols.
//
// Returns:
//
//	[]string: List of exchange IDs.
//	error: An error if scanning fails.
func (c *RedisSymbolCache) GetCachedExchanges() ([]string, error) {
	ctx := context.Background()
	pattern := c.prefix + "*"

	// Get all keys matching the pattern using SCAN for better performance
	var keys []string
	iter := c.redis.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("error scanning cache keys: %w", err)
	}

	// Extract exchange IDs from keys
	var exchanges []string
	prefixLen := len(c.prefix)
	for _, key := range keys {
		if len(key) > prefixLen {
			exchangeID := key[prefixLen:]
			exchanges = append(exchanges, exchangeID)
		}
	}

	return exchanges, nil
}

// WarmCache pre-loads symbols for specified exchanges.
//
// Parameters:
//
//	exchanges: List of exchanges to warm.
//	symbolFetcher: Function to fetch symbols if not cached.
//
// Returns:
//
//	error: An error if warming fails.
func (c *RedisSymbolCache) WarmCache(exchanges []string, symbolFetcher func(string) ([]string, error)) error {
	zaplogrus.Infof("Warming symbol cache for %d exchanges...", len(exchanges))

	for _, exchangeID := range exchanges {
		// Check if already cached
		if _, exists := c.Get(exchangeID); exists {
			zaplogrus.Warnf("Symbols for %s already cached, skipping", exchangeID)
			continue
		}

		// Fetch and cache symbols
		symbols, err := symbolFetcher(exchangeID)
		if err != nil {
			zaplogrus.Warnf("Warning: Failed to warm cache for %s: %v", exchangeID, err)
			continue
		}

		c.Set(exchangeID, symbols)
		zaplogrus.Infof("Warmed cache for %s with %d symbols", exchangeID, len(symbols))
	}

	zaplogrus.Infof("Symbol cache warming completed")
	return nil
}
