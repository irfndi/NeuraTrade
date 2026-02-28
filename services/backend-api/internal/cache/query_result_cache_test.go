package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQueryResultCache(t *testing.T) {
	// Test with nil redis client
	cache := NewQueryResultCache(nil, time.Minute)
	assert.Nil(t, cache)

	// Test with valid redis client
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	cache = NewQueryResultCache(client, time.Minute)
	require.NotNil(t, cache)
	assert.Equal(t, time.Minute, cache.ttl)
	assert.Equal(t, "query_cache:", cache.prefix)
	assert.NotNil(t, cache.stats)
}

func TestQueryResultCache_SetAndGet(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	cache := NewQueryResultCache(client, time.Minute)
	require.NotNil(t, cache)

	ctx := context.Background()

	// Test Set
	testData := map[string]interface{}{
		"test":  "data",
		"value": 123,
	}
	err := cache.Set(ctx, "test_hash", "test_table", testData, 1)
	require.NoError(t, err)

	// Verify stats updated
	assert.Equal(t, int64(1), cache.stats.Sets)

	// Test Get
	entry, found := cache.Get(ctx, "test_hash", "test_table")
	assert.True(t, found)
	require.NotNil(t, entry)
	assert.Equal(t, "test_hash", entry.QueryHash)
	assert.Equal(t, "test_table", entry.TableName)
	assert.Equal(t, 1, entry.RowCount)

	// Verify stats updated
	assert.Equal(t, int64(1), cache.stats.Hits)
}

func TestQueryResultCache_Get_NotFound(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	cache := NewQueryResultCache(client, time.Minute)
	require.NotNil(t, cache)

	ctx := context.Background()

	// Test Get with non-existent key
	entry, found := cache.Get(ctx, "non_existent", "test_table")
	assert.False(t, found)
	assert.Nil(t, entry)

	// Verify stats updated
	assert.Equal(t, int64(1), cache.stats.Misses)
}

func TestQueryResultCache_Get_Expired(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	// Create cache with very short TTL
	cache := NewQueryResultCache(client, 1*time.Millisecond)
	require.NotNil(t, cache)

	ctx := context.Background()

	// Set data
	err := cache.Set(ctx, "test_hash", "test_table", "test_data", 1)
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Try to get expired data
	entry, found := cache.Get(ctx, "test_hash", "test_table")
	assert.False(t, found)
	assert.Nil(t, entry)

	// Verify stats - should count as miss
	assert.Equal(t, int64(1), cache.stats.Misses)
}

func TestQueryResultCache_Invalidate(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	cache := NewQueryResultCache(client, time.Minute)
	require.NotNil(t, cache)

	ctx := context.Background()

	// Set multiple entries for the same table
	err := cache.Set(ctx, "hash1", "test_table", "data1", 1)
	require.NoError(t, err)
	err = cache.Set(ctx, "hash2", "test_table", "data2", 1)
	require.NoError(t, err)
	err = cache.Set(ctx, "hash3", "other_table", "data3", 1)
	require.NoError(t, err)

	// Invalidate all entries for test_table
	err = cache.Invalidate(ctx, "test_table")
	require.NoError(t, err)

	// Verify test_table entries are gone
	_, found := cache.Get(ctx, "hash1", "test_table")
	assert.False(t, found)
	_, found = cache.Get(ctx, "hash2", "test_table")
	assert.False(t, found)

	// Verify other_table entry still exists
	entry, found := cache.Get(ctx, "hash3", "other_table")
	assert.True(t, found)
	assert.NotNil(t, entry)
}

func TestQueryResultCache_InvalidateByPattern(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	cache := NewQueryResultCache(client, time.Minute)
	require.NotNil(t, cache)

	ctx := context.Background()

	// Set multiple entries
	err := cache.Set(ctx, "hash1", "users", "data1", 1)
	require.NoError(t, err)
	err = cache.Set(ctx, "hash2", "users", "data2", 1)
	require.NoError(t, err)
	err = cache.Set(ctx, "hash3", "orders", "data3", 1)
	require.NoError(t, err)

	// Invalidate by pattern
	err = cache.InvalidateByPattern(ctx, "*user*")
	require.NoError(t, err)

	// Verify users entries are gone
	_, found := cache.Get(ctx, "hash1", "users")
	assert.False(t, found)
	_, found = cache.Get(ctx, "hash2", "users")
	assert.False(t, found)

	// Verify orders entry still exists
	entry, found := cache.Get(ctx, "hash3", "orders")
	assert.True(t, found)
	assert.NotNil(t, entry)
}

func TestQueryResultCache_GetStats(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	cache := NewQueryResultCache(client, time.Minute)
	require.NotNil(t, cache)

	ctx := context.Background()

	// Set some data
	err := cache.Set(ctx, "hash1", "test", "data1", 1)
	require.NoError(t, err)

	// Get existing (hit)
	cache.Get(ctx, "hash1", "test")

	// Get non-existing (miss)
	cache.Get(ctx, "hash2", "test")

	// Get stats
	stats := cache.GetStats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Equal(t, int64(1), stats.Sets)
}

func TestQueryResultCache_HitRate(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	cache := NewQueryResultCache(client, time.Minute)
	require.NotNil(t, cache)

	ctx := context.Background()

	// No operations yet - hit rate should be 0
	hitRate := cache.HitRate()
	assert.Equal(t, 0.0, hitRate)

	// Set and get some data
	err := cache.Set(ctx, "hash1", "test", "data", 1)
	require.NoError(t, err)

	// Get existing (hit)
	cache.Get(ctx, "hash1", "test")

	// Get non-existing (miss)
	cache.Get(ctx, "hash2", "test")

	// Hit rate should be 50%
	hitRate = cache.HitRate()
	assert.Equal(t, 50.0, hitRate)
}

func TestQueryResultCache_Clear(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	cache := NewQueryResultCache(client, time.Minute)
	require.NotNil(t, cache)

	ctx := context.Background()

	// Set multiple entries
	err := cache.Set(ctx, "hash1", "table1", "data1", 1)
	require.NoError(t, err)
	err = cache.Set(ctx, "hash2", "table2", "data2", 1)
	require.NoError(t, err)

	// Clear all
	err = cache.Clear(ctx)
	require.NoError(t, err)

	// Verify all entries are gone
	_, found := cache.Get(ctx, "hash1", "table1")
	assert.False(t, found)
	_, found = cache.Get(ctx, "hash2", "table2")
	assert.False(t, found)
}
