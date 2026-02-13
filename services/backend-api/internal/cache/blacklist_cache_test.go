package cache

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// setupBlacklistTestRedis creates a test Redis instance using miniredis
func setupBlacklistTestRedis(t *testing.T) *redis.Client {
	s, err := miniredis.Run()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("miniredis cannot bind in this environment; skipping Redis-backed blacklist tests")
		}
		require.NoError(t, err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	// Store the miniredis instance in the test's cleanup
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close Redis client: %v", err)
		}
		s.Close()
	})

	return client
}

// cleanupBlacklistTestRedis is kept for compatibility but cleanup is now handled by t.Cleanup
func cleanupBlacklistTestRedis(t *testing.T, client *redis.Client) {
	// Cleanup is now handled by t.Cleanup in setupBlacklistTestRedis
	// This function is kept for backward compatibility
}

// TestInMemoryBlacklistCache tests the in-memory implementation
func TestInMemoryBlacklistCache(t *testing.T) {
	cache := NewInMemoryBlacklistCache()

	// Test Add and IsBlacklisted
	cache.Add("binance", "test blacklist", time.Hour)
	isBlacklisted, reason := cache.IsBlacklisted("binance")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "test blacklist", reason)

	// Test Remove
	cache.Remove("binance")
	isBlacklisted, _ = cache.IsBlacklisted("binance")
	assert.False(t, isBlacklisted)

	// Test Clear
	cache.Add("binance", "test1", time.Hour)
	cache.Add("coinbase", "test2", time.Hour)
	cache.Clear()
	isBlacklisted, _ = cache.IsBlacklisted("binance")
	assert.False(t, isBlacklisted)
	isBlacklisted, _ = cache.IsBlacklisted("coinbase")
	assert.False(t, isBlacklisted)
}

// TestBlacklistCacheExpiration tests TTL functionality
func TestBlacklistCacheExpiration(t *testing.T) {
	cache := NewInMemoryBlacklistCache()

	// Add with very short TTL
	cache.Add("binance", "test blacklist", 10*time.Millisecond)

	// Should be blacklisted immediately
	isBlacklisted, reason := cache.IsBlacklisted("binance")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "test blacklist", reason)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should no longer be blacklisted
	isBlacklisted, _ = cache.IsBlacklisted("binance")
	assert.False(t, isBlacklisted)
}

// TestBlacklistCacheStats tests statistics tracking
func TestBlacklistCacheStats(t *testing.T) {
	cache := NewInMemoryBlacklistCache()

	// Add some entries
	cache.Add("binance", "test1", time.Hour)
	cache.Add("coinbase", "test2", time.Hour)

	// Check some entries (hits and misses)
	cache.IsBlacklisted("binance")  // hit
	cache.IsBlacklisted("coinbase") // hit
	cache.IsBlacklisted("unknown")  // miss

	stats := cache.GetStats()
	assert.Equal(t, int64(2), stats.TotalEntries)
	assert.Equal(t, int64(2), stats.Adds)
	assert.Equal(t, int64(2), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
}

// TestBlacklistPersistenceInterface tests that the interface supports persistence
func TestBlacklistPersistenceInterface(t *testing.T) {
	// Test that RedisBlacklistCache implements the interface correctly
	cache := NewInMemoryBlacklistCache()

	// Test basic operations
	cache.Add("test", "reason", time.Hour)
	isBlacklisted, reason := cache.IsBlacklisted("test")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "reason", reason)

	// Test that LoadFromDatabase method exists (for in-memory cache, it returns an error)
	err := cache.LoadFromDatabase(context.Background())
	// For in-memory cache, this should return an error indicating no database support
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database persistence not supported")

	// Test GetBlacklistedSymbols method exists
	symbols, err := cache.GetBlacklistedSymbols()
	assert.NoError(t, err)
	assert.Len(t, symbols, 1)
	assert.Equal(t, "test", symbols[0].Symbol)
	assert.Equal(t, "reason", symbols[0].Reason)
}

// TestDatabasePersistenceFlow tests the expected flow for database persistence
func TestDatabasePersistenceFlow(t *testing.T) {
	// This test documents the expected behavior for database persistence
	// without requiring actual database connection

	// 1. Application starts and loads blacklist from database
	cache := NewInMemoryBlacklistCache()
	err := cache.LoadFromDatabase(context.Background())
	assert.Error(t, err) // In-memory cache should return error for database operations
	assert.Contains(t, err.Error(), "database persistence not supported")

	// 2. Add exchange to blacklist (should persist to database in real implementation)
	cache.Add("binance", "Manual blacklist via API", 0) // 0 = no expiration

	// 3. Verify it's blacklisted
	isBlacklisted, reason := cache.IsBlacklisted("binance")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "Manual blacklist via API", reason)

	// 4. Remove from blacklist (should remove from database in real implementation)
	cache.Remove("binance")

	// 5. Verify it's no longer blacklisted
	isBlacklisted, _ = cache.IsBlacklisted("binance")
	assert.False(t, isBlacklisted)

	// 6. Get all blacklisted symbols
	symbols, err := cache.GetBlacklistedSymbols()
	assert.NoError(t, err)
	assert.Empty(t, symbols) // Should be empty after removal
}

// TestRedisBlacklistCache_NewRedisBlacklistCache tests the constructor
func TestRedisBlacklistCache_NewRedisBlacklistCache(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	assert.NotNil(t, cache)
	assert.Equal(t, client, cache.client)
	assert.Equal(t, "blacklist:", cache.prefix)
}

// TestRedisBlacklistCache_IsBlacklisted tests checking if a symbol is blacklisted
func TestRedisBlacklistCache_IsBlacklisted(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call
	repo.On("AddExchange", mock.Anything, "test-symbol", "test reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "test-symbol",
		Reason:       "test reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Test non-blacklisted symbol
	isBlacklisted, reason := cache.IsBlacklisted("nonexistent")
	assert.False(t, isBlacklisted)
	assert.Empty(t, reason)

	// Add a symbol to blacklist
	cache.Add("test-symbol", "test reason", time.Hour)

	// Test blacklisted symbol
	isBlacklisted, reason = cache.IsBlacklisted("test-symbol")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "test reason", reason)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_IsBlacklisted_Expired tests expired entries
func TestRedisBlacklistCache_IsBlacklisted_Expired(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call
	repo.On("AddExchange", mock.Anything, "expired-symbol", "test reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "expired-symbol",
		Reason:       "test reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add a symbol with very short TTL
	cache.Add("expired-symbol", "test reason", 10*time.Millisecond)

	// Should be blacklisted immediately
	isBlacklisted, reason := cache.IsBlacklisted("expired-symbol")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "test reason", reason)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should no longer be blacklisted
	isBlacklisted, _ = cache.IsBlacklisted("expired-symbol")
	assert.False(t, isBlacklisted)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_Add tests adding symbols to blacklist
func TestRedisBlacklistCache_Add(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call
	repo.On("AddExchange", mock.Anything, "test-symbol", "test reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "test-symbol",
		Reason:       "test reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add symbol with TTL
	cache.Add("test-symbol", "test reason", time.Hour)

	// Verify it's blacklisted
	isBlacklisted, reason := cache.IsBlacklisted("test-symbol")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "test reason", reason)

	// Check stats
	stats := cache.GetStats()
	assert.Equal(t, int64(1), stats.TotalEntries)
	assert.Equal(t, int64(1), stats.Adds)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_Add_NoTTL tests adding symbols without TTL
func TestRedisBlacklistCache_Add_NoTTL(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call
	repo.On("AddExchange", mock.Anything, "permanent-symbol", "permanent reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "permanent-symbol",
		Reason:       "permanent reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add symbol without TTL
	cache.Add("permanent-symbol", "permanent reason", 0)

	// Verify it's blacklisted
	isBlacklisted, reason := cache.IsBlacklisted("permanent-symbol")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "permanent reason", reason)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_Remove tests removing symbols from blacklist
func TestRedisBlacklistCache_Remove(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository calls
	repo.On("AddExchange", mock.Anything, "test-symbol", "test reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "test-symbol",
		Reason:       "test reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("RemoveExchange", mock.Anything, "test-symbol").Return(nil)

	// Add symbol first
	cache.Add("test-symbol", "test reason", time.Hour)

	// Verify it's blacklisted
	isBlacklisted, _ := cache.IsBlacklisted("test-symbol")
	assert.True(t, isBlacklisted)

	// Remove symbol
	cache.Remove("test-symbol")

	// Verify it's no longer blacklisted
	isBlacklisted, _ = cache.IsBlacklisted("test-symbol")
	assert.False(t, isBlacklisted)

	// Check stats
	stats := cache.GetStats()
	assert.Equal(t, int64(0), stats.TotalEntries)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_Remove_NonExistent tests removing non-existent symbol
func TestRedisBlacklistCache_Remove_NonExistent(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call - expect it to be called even for non-existent symbol
	repo.On("RemoveExchange", mock.Anything, "non-existent-symbol").Return(nil)

	// Remove non-existent symbol (should not panic)
	cache.Remove("non-existent-symbol")

	// Stats should remain unchanged
	stats := cache.GetStats()
	assert.Equal(t, int64(0), stats.TotalEntries)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_Clear tests clearing all blacklisted symbols
func TestRedisBlacklistCache_Clear(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository calls
	repo.On("AddExchange", mock.Anything, "symbol1", "reason1", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "symbol1",
		Reason:       "reason1",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "symbol2", "reason2", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           2,
		ExchangeName: "symbol2",
		Reason:       "reason2",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "symbol3", "reason3", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           3,
		ExchangeName: "symbol3",
		Reason:       "reason3",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add multiple symbols
	cache.Add("symbol1", "reason1", time.Hour)
	cache.Add("symbol2", "reason2", time.Hour)
	cache.Add("symbol3", "reason3", time.Hour)

	// Verify they're blacklisted
	isBlacklisted1, _ := cache.IsBlacklisted("symbol1")
	isBlacklisted2, _ := cache.IsBlacklisted("symbol2")
	isBlacklisted3, _ := cache.IsBlacklisted("symbol3")
	assert.True(t, isBlacklisted1)
	assert.True(t, isBlacklisted2)
	assert.True(t, isBlacklisted3)

	// Clear all
	cache.Clear()

	// Verify they're no longer blacklisted
	isBlacklisted1, _ = cache.IsBlacklisted("symbol1")
	isBlacklisted2, _ = cache.IsBlacklisted("symbol2")
	isBlacklisted3, _ = cache.IsBlacklisted("symbol3")
	assert.False(t, isBlacklisted1)
	assert.False(t, isBlacklisted2)
	assert.False(t, isBlacklisted3)

	// Check stats
	stats := cache.GetStats()
	assert.Equal(t, int64(0), stats.TotalEntries)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_Clear_Empty tests clearing empty blacklist
func TestRedisBlacklistCache_Clear_Empty(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Clear empty blacklist (should not panic)
	cache.Clear()

	// Stats should remain unchanged
	stats := cache.GetStats()
	assert.Equal(t, int64(0), stats.TotalEntries)
}

// TestRedisBlacklistCache_GetStats tests getting cache statistics
func TestRedisBlacklistCache_GetStats(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository calls
	repo.On("AddExchange", mock.Anything, "symbol1", "reason1", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "symbol1",
		Reason:       "reason1",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "symbol2", "reason2", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           2,
		ExchangeName: "symbol2",
		Reason:       "reason2",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Get initial stats
	stats := cache.GetStats()
	assert.Equal(t, int64(0), stats.TotalEntries)
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
	assert.Equal(t, int64(0), stats.Adds)

	// Add some symbols and perform operations
	cache.Add("symbol1", "reason1", time.Hour)
	cache.Add("symbol2", "reason2", time.Hour)
	cache.IsBlacklisted("symbol1")     // hit
	cache.IsBlacklisted("symbol2")     // hit
	cache.IsBlacklisted("nonexistent") // miss

	// Get updated stats
	stats = cache.GetStats()
	assert.Equal(t, int64(2), stats.TotalEntries)
	assert.Equal(t, int64(2), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Equal(t, int64(2), stats.Adds)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_LogStats tests logging cache statistics
func TestRedisBlacklistCache_LogStats(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call
	repo.On("AddExchange", mock.Anything, "test-symbol", "test reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "test-symbol",
		Reason:       "test reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add some activity
	cache.Add("test-symbol", "test reason", time.Hour)
	cache.IsBlacklisted("test-symbol")
	cache.IsBlacklisted("nonexistent")

	// Log stats (should not panic)
	cache.LogStats()

	// Verify stats are still accessible
	stats := cache.GetStats()
	assert.Equal(t, int64(1), stats.TotalEntries)
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_Close tests closing the cache
func TestRedisBlacklistCache_Close(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call
	repo.On("AddExchange", mock.Anything, "test-symbol", "test reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "test-symbol",
		Reason:       "test reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add some data
	cache.Add("test-symbol", "test reason", time.Hour)

	// Close cache (should not panic)
	err := cache.Close()
	assert.NoError(t, err)

	// Verify cache still works (Redis client is managed externally)
	isBlacklisted, _ := cache.IsBlacklisted("test-symbol")
	assert.True(t, isBlacklisted)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_GetBlacklistedSymbols tests getting all blacklisted symbols
func TestRedisBlacklistCache_GetBlacklistedSymbols(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository calls
	repo.On("AddExchange", mock.Anything, "symbol1", "reason1", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "symbol1",
		Reason:       "reason1",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "symbol2", "reason2", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           2,
		ExchangeName: "symbol2",
		Reason:       "reason2",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add some symbols
	cache.Add("symbol1", "reason1", time.Hour)
	cache.Add("symbol2", "reason2", 2*time.Hour)

	// Get all blacklisted symbols
	symbols, err := cache.GetBlacklistedSymbols()
	assert.NoError(t, err)
	assert.Len(t, symbols, 2)

	// Verify symbols are correct
	symbolMap := make(map[string]BlacklistCacheEntry)
	for _, symbol := range symbols {
		symbolMap[symbol.Symbol] = symbol
	}

	assert.Contains(t, symbolMap, "symbol1")
	assert.Contains(t, symbolMap, "symbol2")
	assert.Equal(t, "reason1", symbolMap["symbol1"].Reason)
	assert.Equal(t, "reason2", symbolMap["symbol2"].Reason)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_GetBlacklistedSymbols_Empty tests getting symbols from empty cache
func TestRedisBlacklistCache_GetBlacklistedSymbols_Empty(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Get all blacklisted symbols from empty cache
	symbols, err := cache.GetBlacklistedSymbols()
	assert.NoError(t, err)
	assert.Empty(t, symbols)
}

// TestRedisBlacklistCache_GetBlacklistedSymbols_WithExpired tests getting symbols with expired entries
func TestRedisBlacklistCache_GetBlacklistedSymbols_WithExpired(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository calls
	repo.On("AddExchange", mock.Anything, "active-symbol", "active reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "active-symbol",
		Reason:       "active reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "expired-symbol", "expired reason", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           2,
		ExchangeName: "expired-symbol",
		Reason:       "expired reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add symbols with different TTLs
	cache.Add("active-symbol", "active reason", time.Hour)
	cache.Add("expired-symbol", "expired reason", 10*time.Millisecond)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Get all blacklisted symbols (expired ones should be filtered out)
	symbols, err := cache.GetBlacklistedSymbols()
	assert.NoError(t, err)
	assert.Len(t, symbols, 1)

	// Only active symbol should be returned
	assert.Equal(t, "active-symbol", symbols[0].Symbol)
	assert.Equal(t, "active reason", symbols[0].Reason)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_CleanupExpired tests cleaning up expired entries
func TestRedisBlacklistCache_CleanupExpired(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository calls
	repo.On("AddExchange", mock.Anything, "active1", "reason1", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "active1",
		Reason:       "reason1",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "active2", "reason2", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           2,
		ExchangeName: "active2",
		Reason:       "reason2",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "expired1", "expired1", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           3,
		ExchangeName: "expired1",
		Reason:       "expired1",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "expired2", "expired2", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           4,
		ExchangeName: "expired2",
		Reason:       "expired2",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add symbols with different TTLs
	cache.Add("active1", "reason1", time.Hour)
	cache.Add("active2", "reason2", time.Hour)
	cache.Add("expired1", "expired1", 10*time.Millisecond)
	cache.Add("expired2", "expired2", 10*time.Millisecond)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Cleanup expired entries
	expiredCount := cache.CleanupExpired()
	assert.Equal(t, 2, expiredCount)

	// Verify only active symbols remain
	isActive1, _ := cache.IsBlacklisted("active1")
	isActive2, _ := cache.IsBlacklisted("active2")
	isExpired1, _ := cache.IsBlacklisted("expired1")
	isExpired2, _ := cache.IsBlacklisted("expired2")
	assert.True(t, isActive1)
	assert.True(t, isActive2)
	assert.False(t, isExpired1)
	assert.False(t, isExpired2)

	// Check stats
	stats := cache.GetStats()
	assert.Equal(t, int64(2), stats.TotalEntries)
	assert.Equal(t, int64(2), stats.ExpiredEntries)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_CleanupExpired_NoExpired tests cleanup when no entries are expired
func TestRedisBlacklistCache_CleanupExpired_NoExpired(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository calls
	repo.On("AddExchange", mock.Anything, "active1", "reason1", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "active1",
		Reason:       "reason1",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)
	repo.On("AddExchange", mock.Anything, "active2", "reason2", mock.Anything).Return(&database.ExchangeBlacklistEntry{
		ID:           2,
		ExchangeName: "active2",
		Reason:       "reason2",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     true,
	}, nil)

	// Add active symbols
	cache.Add("active1", "reason1", time.Hour)
	cache.Add("active2", "reason2", time.Hour)

	// Cleanup expired entries
	expiredCount := cache.CleanupExpired()
	assert.Equal(t, 0, expiredCount)

	// Verify all symbols remain
	isActive1, _ := cache.IsBlacklisted("active1")
	isActive2, _ := cache.IsBlacklisted("active2")
	assert.True(t, isActive1)
	assert.True(t, isActive2)

	// Check stats
	stats := cache.GetStats()
	assert.Equal(t, int64(2), stats.TotalEntries)
	assert.Equal(t, int64(0), stats.ExpiredEntries)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_LoadFromDatabase tests loading blacklist entries from database
func TestRedisBlacklistCache_LoadFromDatabase(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call to return blacklisted entries
	now := time.Now()
	futureTime := now.Add(time.Hour)

	repo.On("GetAllBlacklisted", mock.Anything).Return([]database.ExchangeBlacklistEntry{
		{
			ID:           1,
			ExchangeName: "binance",
			Reason:       "high latency",
			CreatedAt:    now,
			UpdatedAt:    now,
			ExpiresAt:    &futureTime,
			IsActive:     true,
		},
		{
			ID:           2,
			ExchangeName: "coinbase",
			Reason:       "api issues",
			CreatedAt:    now,
			UpdatedAt:    now,
			ExpiresAt:    nil, // No expiration
			IsActive:     true,
		},
		{
			ID:           3,
			ExchangeName: "kraken",
			Reason:       "maintenance",
			CreatedAt:    now,
			UpdatedAt:    now,
			ExpiresAt:    &futureTime,
			IsActive:     true,
		},
	}, nil)

	// Load from database
	err := cache.LoadFromDatabase(context.Background())
	assert.NoError(t, err)

	// Verify entries were loaded
	isBlacklisted, reason := cache.IsBlacklisted("binance")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "high latency", reason)

	isBlacklisted, reason = cache.IsBlacklisted("coinbase")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "api issues", reason)

	isBlacklisted, reason = cache.IsBlacklisted("kraken")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "maintenance", reason)

	// Verify non-existent symbol
	isBlacklisted, _ = cache.IsBlacklisted("nonexistent")
	assert.False(t, isBlacklisted)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_LoadFromDatabase_Empty tests loading from database when no entries exist
func TestRedisBlacklistCache_LoadFromDatabase_Empty(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call to return empty list
	repo.On("GetAllBlacklisted", mock.Anything).Return([]database.ExchangeBlacklistEntry{}, nil)

	// Load from database
	err := cache.LoadFromDatabase(context.Background())
	assert.NoError(t, err)

	// Verify no entries were loaded
	isBlacklisted, _ := cache.IsBlacklisted("nonexistent")
	assert.False(t, isBlacklisted)

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_LoadFromDatabase_Error tests loading from database when there's an error
func TestRedisBlacklistCache_LoadFromDatabase_Error(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	// Mock the repository call to return an error
	repo.On("GetAllBlacklisted", mock.Anything).Return([]database.ExchangeBlacklistEntry{}, fmt.Errorf("database connection failed"))

	// Load from database
	err := cache.LoadFromDatabase(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load blacklist from database")

	repo.AssertExpectations(t)
}

// TestRedisBlacklistCache_LoadFromDatabase_WithExpired tests that expired entries are not loaded
func TestRedisBlacklistCache_LoadFromDatabase_WithExpired(t *testing.T) {
	client := setupBlacklistTestRedis(t)
	defer cleanupBlacklistTestRedis(t, client)

	repo := &MockBlacklistRepository{}
	cache := NewRedisBlacklistCache(client, repo)

	now := time.Now()
	pastTime := now.Add(-time.Hour)  // Expired
	futureTime := now.Add(time.Hour) // Not expired

	repo.On("GetAllBlacklisted", mock.Anything).Return([]database.ExchangeBlacklistEntry{
		{
			ID:           1,
			ExchangeName: "active-exchange",
			Reason:       "active reason",
			CreatedAt:    now,
			UpdatedAt:    now,
			ExpiresAt:    &futureTime,
			IsActive:     true,
		},
		{
			ID:           2,
			ExchangeName: "expired-exchange",
			Reason:       "expired reason",
			CreatedAt:    now,
			UpdatedAt:    now,
			ExpiresAt:    &pastTime,
			IsActive:     true,
		},
	}, nil)

	// Load from database
	err := cache.LoadFromDatabase(context.Background())
	assert.NoError(t, err)

	// Verify only active entry was loaded
	isBlacklisted, reason := cache.IsBlacklisted("active-exchange")
	assert.True(t, isBlacklisted)
	assert.Equal(t, "active reason", reason)

	// Verify expired entry was not loaded
	isBlacklisted, _ = cache.IsBlacklisted("expired-exchange")
	assert.False(t, isBlacklisted)

	repo.AssertExpectations(t)
}

// TestInMemoryBlacklistCache_LogStats tests logging statistics for in-memory cache
func TestInMemoryBlacklistCache_LogStats(t *testing.T) {
	cache := NewInMemoryBlacklistCache()

	// Add some entries to create stats
	cache.Add("binance", "test reason", time.Hour)
	cache.Add("coinbase", "test reason", time.Hour)

	// Test LogStats doesn't panic
	// Note: We can't easily test the log output without capturing stdout
	assert.NotPanics(t, func() {
		cache.LogStats()
	})

	// Verify stats are still accessible
	stats := cache.GetStats()
	assert.Equal(t, int64(2), stats.TotalEntries)
	assert.Equal(t, int64(2), stats.Adds)
}

// TestInMemoryBlacklistCache_Close tests closing the in-memory cache
func TestInMemoryBlacklistCache_Close(t *testing.T) {
	cache := NewInMemoryBlacklistCache()

	// Add some entries
	cache.Add("binance", "test reason", time.Hour)

	// Test Close doesn't panic and returns nil error
	assert.NotPanics(t, func() {
		err := cache.Close()
		assert.NoError(t, err)
	})

	// Verify cache still works after close (in-memory implementation doesn't actually close)
	isBlacklisted, _ := cache.IsBlacklisted("binance")
	assert.True(t, isBlacklisted)
}

// TestBlacklistRepositoryInterface tests the expected repository interface
func TestBlacklistRepositoryInterface(t *testing.T) {
	// This test documents the expected repository interface
	// The actual repository implementation is tested separately

	// Expected methods that should be available:
	// - AddExchange(ctx, exchangeName, reason, expiresAt) (*ExchangeBlacklistEntry, error)
	// - RemoveExchange(ctx, exchangeName) error
	// - IsBlacklisted(ctx, exchangeName) (bool, string, error)
	// - GetAllBlacklisted(ctx) ([]ExchangeBlacklistEntry, error)
	// - CleanupExpired(ctx) (int64, error)
	// - GetBlacklistHistory(ctx, limit) ([]ExchangeBlacklistEntry, error)

	// Test that the ExchangeBlacklistEntry struct has expected fields
	entry := database.ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "binance",
		Reason:       "test",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		ExpiresAt:    nil,
		IsActive:     true,
	}

	assert.Equal(t, int64(1), entry.ID)
	assert.Equal(t, "binance", entry.ExchangeName)
	assert.Equal(t, "test", entry.Reason)
	assert.True(t, entry.IsActive)
	assert.Nil(t, entry.ExpiresAt)
}
