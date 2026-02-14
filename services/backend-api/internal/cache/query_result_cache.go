package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type QueryResultCacheEntry struct {
	Data      interface{} `json:"data"`
	CachedAt  time.Time   `json:"cached_at"`
	ExpiresAt time.Time   `json:"expires_at"`
	QueryHash string      `json:"query_hash"`
	TableName string      `json:"table_name"`
	RowCount  int         `json:"row_count"`
}

type QueryResultCacheStats struct {
	Hits   int64 `json:"hits"`
	Misses int64 `json:"misses"`
	Sets   int64 `json:"sets"`
	mu     sync.RWMutex
}

type QueryResultCache struct {
	redis     *redis.Client
	ttl       time.Duration
	stats     *QueryResultCacheStats
	prefix    string
	enableLog bool
}

func NewQueryResultCache(redisClient *redis.Client, ttl time.Duration) *QueryResultCache {
	if redisClient == nil {
		return nil
	}
	return &QueryResultCache{
		redis:     redisClient,
		ttl:       ttl,
		stats:     &QueryResultCacheStats{},
		prefix:    "query_cache:",
		enableLog: true,
	}
}

func (c *QueryResultCache) Get(ctx context.Context, queryHash, tableName string) (*QueryResultCacheEntry, bool) {
	cacheKey := c.prefix + tableName + ":" + queryHash

	data, err := c.redis.Get(ctx, cacheKey).Result()
	if err == redis.Nil {
		c.stats.mu.Lock()
		c.stats.Misses++
		c.stats.mu.Unlock()
		return nil, false
	}
	if err != nil {
		if c.enableLog {
			log.Printf("QueryResultCache Redis error: %v", err)
		}
		c.stats.mu.Lock()
		c.stats.Misses++
		c.stats.mu.Unlock()
		return nil, false
	}

	var entry QueryResultCacheEntry
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		if c.enableLog {
			log.Printf("QueryResultCache unmarshal error: %v", err)
		}
		c.stats.mu.Lock()
		c.stats.Misses++
		c.stats.mu.Unlock()
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) {
		if c.enableLog {
			log.Printf("QueryResultCache entry expired for %s", tableName)
		}
		c.stats.mu.Lock()
		c.stats.Misses++
		c.stats.mu.Unlock()
		return nil, false
	}

	c.stats.mu.Lock()
	c.stats.Hits++
	c.stats.mu.Unlock()

	return &entry, true
}

func (c *QueryResultCache) Set(ctx context.Context, queryHash, tableName string, data interface{}, rowCount int) error {
	cacheKey := c.prefix + tableName + ":" + queryHash

	now := time.Now()
	entry := QueryResultCacheEntry{
		Data:      data,
		CachedAt:  now,
		ExpiresAt: now.Add(c.ttl),
		QueryHash: queryHash,
		TableName: tableName,
		RowCount:  rowCount,
	}

	dataBytes, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal query cache entry: %w", err)
	}

	if err := c.redis.Set(ctx, cacheKey, dataBytes, c.ttl).Err(); err != nil {
		return fmt.Errorf("failed to set query cache: %w", err)
	}

	c.stats.mu.Lock()
	c.stats.Sets++
	c.stats.mu.Unlock()

	if c.enableLog {
		hashPreview := queryHash
		if len(hashPreview) > 8 {
			hashPreview = hashPreview[:8]
		}
		log.Printf("Cached query result for %s (hash: %s, rows: %d, TTL: %v)",
			tableName, hashPreview, rowCount, c.ttl)
	}

	return nil
}

func (c *QueryResultCache) Invalidate(ctx context.Context, tableName string) error {
	pattern := c.prefix + tableName + ":*"

	iter := c.redis.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if len(keys) > 0 {
		if err := c.redis.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to invalidate cache: %w", err)
		}
		if c.enableLog {
			log.Printf("Invalidated %d cache entries for table %s", len(keys), tableName)
		}
	}

	return nil
}

func (c *QueryResultCache) InvalidateByPattern(ctx context.Context, pattern string) error {
	searchPattern := c.prefix + pattern

	iter := c.redis.Scan(ctx, 0, searchPattern, 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if len(keys) > 0 {
		if err := c.redis.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to invalidate cache: %w", err)
		}
		if c.enableLog {
			log.Printf("Invalidated %d cache entries matching pattern %s", len(keys), pattern)
		}
	}

	return nil
}

func (c *QueryResultCache) GetStats() QueryResultCacheStats {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()
	return QueryResultCacheStats{
		Hits:   c.stats.Hits,
		Misses: c.stats.Misses,
		Sets:   c.stats.Sets,
	}
}

func (c *QueryResultCache) HitRate() float64 {
	stats := c.GetStats()
	total := stats.Hits + stats.Misses
	if total == 0 {
		return 0
	}
	return float64(stats.Hits) / float64(total) * 100
}

func (c *QueryResultCache) Clear(ctx context.Context) error {
	pattern := c.prefix + "*"

	iter := c.redis.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if len(keys) > 0 {
		if err := c.redis.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to clear cache: %w", err)
		}
		log.Printf("Cleared %d query cache entries", len(keys))
	}

	return nil
}
