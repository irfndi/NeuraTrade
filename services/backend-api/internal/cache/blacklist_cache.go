package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/redis/go-redis/v9"
)

// BlacklistCacheEntry represents a blacklisted symbol with metadata.
type BlacklistCacheEntry struct {
	// Symbol is the trading pair identifier that is blacklisted.
	Symbol string `json:"symbol"`
	// Reason describes why the symbol was blacklisted.
	Reason string `json:"reason"`
	// ExpiresAt points to the time when the blacklist entry is no longer valid.
	// If nil, the entry does not expire automatically.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// CreatedAt is the timestamp when the entry was created.
	CreatedAt time.Time `json:"created_at"`
}

// BlacklistCacheStats holds statistics about the blacklist cache.
type BlacklistCacheStats struct {
	// TotalEntries is the current number of items in the cache.
	TotalEntries int64 `json:"total_entries"`
	// ExpiredEntries is the count of entries that have passed their expiration time.
	ExpiredEntries int64 `json:"expired_entries"`
	// Hits is the number of successful cache lookups.
	Hits int64 `json:"hits"`
	// Misses is the number of failed cache lookups.
	Misses int64 `json:"misses"`
	// Adds is the total number of entries added to the cache.
	Adds int64 `json:"adds"`
	// LastCleanup is the timestamp of the last cache cleanup operation.
	LastCleanup time.Time `json:"last_cleanup"`
}

// BlacklistCache interface defines the contract for blacklist caching mechanisms.
type BlacklistCache interface {
	// IsBlacklisted checks if a symbol is in the blacklist.
	IsBlacklisted(symbol string) (bool, string)
	// Add adds a symbol to the blacklist with a reason and time-to-live.
	Add(symbol, reason string, ttl time.Duration)
	// Remove removes a symbol from the blacklist.
	Remove(symbol string)
	// Clear removes all entries from the blacklist cache.
	Clear()
	// GetStats returns the current cache statistics.
	GetStats() BlacklistCacheStats
	// LogStats logs the current cache statistics.
	LogStats()
	// Close cleans up any resources associated with the cache.
	Close() error
	// LoadFromDatabase populates the cache from the persistent store.
	LoadFromDatabase(ctx context.Context) error
	// GetBlacklistedSymbols retrieves all blacklisted symbols from the cache.
	GetBlacklistedSymbols() ([]BlacklistCacheEntry, error)
}

// BlacklistRepository interface defines the contract for database operations.
// This allows for dependency injection and testing with mock implementations.
type BlacklistRepository interface {
	// AddExchange adds an exchange to the database blacklist.
	AddExchange(ctx context.Context, exchangeName, reason string, expiresAt *time.Time) (*database.ExchangeBlacklistEntry, error)
	// RemoveExchange removes an exchange from the database blacklist.
	RemoveExchange(ctx context.Context, exchangeName string) error
	// IsBlacklisted checks if an exchange is blacklisted in the database.
	IsBlacklisted(ctx context.Context, exchangeName string) (bool, string, error)
	// GetAllBlacklisted retrieves all blacklisted exchanges from the database.
	GetAllBlacklisted(ctx context.Context) ([]database.ExchangeBlacklistEntry, error)
	// CleanupExpired removes expired entries from the database.
	CleanupExpired(ctx context.Context) (int64, error)
	// GetBlacklistHistory retrieves the blacklist history from the database.
	GetBlacklistHistory(ctx context.Context, limit int) ([]database.ExchangeBlacklistEntry, error)
	// ClearAll removes all entries from the database blacklist.
	ClearAll(ctx context.Context) (int64, error)
}

// RedisBlacklistCache implements BlacklistCache using Redis with database persistence.
// It manages the synchronization between the Redis cache and the database repository.
type RedisBlacklistCache struct {
	client redis.Cmdable
	ctx    context.Context
	stats  BlacklistCacheStats
	mu     sync.RWMutex
	prefix string
	repo   BlacklistRepository
}

// NewRedisBlacklistCache creates a new Redis-based blacklist cache with database persistence.
//
// Parameters:
//
//	client: The Redis client interface.
//	repo: The database repository interface.
//
// Returns:
//
//	*RedisBlacklistCache: A pointer to the initialized RedisBlacklistCache.
func NewRedisBlacklistCache(client redis.Cmdable, repo BlacklistRepository) *RedisBlacklistCache {
	return &RedisBlacklistCache{
		client: client,
		ctx:    context.Background(),
		prefix: "blacklist:",
		stats:  BlacklistCacheStats{},
		repo:   repo,
	}
}

// IsBlacklisted checks if a symbol is blacklisted.
// It looks up the symbol in Redis and checks expiration.
//
// Parameters:
//
//	symbol: The symbol to check.
//
// Returns:
//
//	bool: True if blacklisted.
//	string: The reason for blacklisting.
func (rbc *RedisBlacklistCache) IsBlacklisted(symbol string) (bool, string) {
	rbc.mu.Lock()
	defer rbc.mu.Unlock()

	key := rbc.prefix + symbol
	val, err := rbc.client.Get(rbc.ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			rbc.stats.Misses++
			return false, ""
		}
		// Log error but don't fail the check
		log.Printf("Redis blacklist check error for %s: %v", symbol, err)
		rbc.stats.Misses++
		return false, ""
	}

	var entry BlacklistCacheEntry
	if err := json.Unmarshal([]byte(val), &entry); err != nil {
		log.Printf("Failed to unmarshal blacklist entry for %s: %v", symbol, err)
		rbc.stats.Misses++
		return false, ""
	}

	// Check if entry has expired
	if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
		// Remove expired entry
		rbc.client.Del(rbc.ctx, key)
		rbc.stats.ExpiredEntries++
		rbc.stats.Misses++
		return false, ""
	}

	rbc.stats.Hits++
	return true, entry.Reason
}

// Add adds a symbol to the blacklist with a TTL.
// It persists the entry to the database and then updates the Redis cache.
//
// Parameters:
//
//	symbol: The symbol to blacklist.
//	reason: The reason for blacklisting.
//	ttl: The time-to-live duration.
func (rbc *RedisBlacklistCache) Add(symbol, reason string, ttl time.Duration) {
	rbc.mu.Lock()
	defer rbc.mu.Unlock()

	entry := BlacklistCacheEntry{
		Symbol:    symbol,
		Reason:    reason,
		CreatedAt: time.Now(),
	}

	// Set expiration time if TTL is provided
	if ttl > 0 {
		expiresAt := time.Now().Add(ttl)
		entry.ExpiresAt = &expiresAt
	}

	// Persist to database first
	if rbc.repo != nil {
		ctx := context.Background()
		_, err := rbc.repo.AddExchange(ctx, symbol, reason, entry.ExpiresAt)
		if err != nil {
			log.Printf("Error persisting to database: %v", err)
			// Continue with Redis cache even if database fails
		}
	}

	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("Failed to marshal blacklist entry for %s: %v", symbol, err)
		return
	}

	key := rbc.prefix + symbol
	err = rbc.client.Set(rbc.ctx, key, data, ttl).Err()
	if err != nil {
		log.Printf("Failed to set blacklist entry for %s: %v", symbol, err)
		return
	}

	rbc.stats.Adds++
	rbc.stats.TotalEntries++
	log.Printf("Blacklisted symbol %s for %s (TTL: %v)", symbol, reason, ttl)
}

// Remove removes a symbol from the blacklist.
// It removes the entry from both the database and the Redis cache.
//
// Parameters:
//
//	symbol: The symbol to remove.
func (rbc *RedisBlacklistCache) Remove(symbol string) {
	rbc.mu.Lock()
	defer rbc.mu.Unlock()

	// Remove from database first
	if rbc.repo != nil {
		ctx := context.Background()
		err := rbc.repo.RemoveExchange(ctx, symbol)
		if err != nil {
			log.Printf("Error removing from database: %v", err)
			// Continue with Redis cache even if database fails
		}
	}

	key := rbc.prefix + symbol
	result := rbc.client.Del(rbc.ctx, key)
	if result.Err() != nil {
		log.Printf("Failed to remove blacklist entry for %s: %v", symbol, result.Err())
		return
	}

	if result.Val() > 0 {
		rbc.stats.TotalEntries--
		log.Printf("Removed symbol %s from blacklist", symbol)
	}
}

// Clear removes all blacklisted symbols from the Redis cache.
// Note: It does not clear the database to prevent accidental data loss.
// A safe clear operation for the repository should be implemented if needed.
func (rbc *RedisBlacklistCache) Clear() {
	rbc.mu.Lock()
	defer rbc.mu.Unlock()

	pattern := rbc.prefix + "*"
	var keys []string
	iter := rbc.client.Scan(rbc.ctx, 0, pattern, 0).Iterator()
	for iter.Next(rbc.ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		log.Printf("Failed to scan blacklist keys: %v", err)
		return
	}

	if len(keys) > 0 {
		result := rbc.client.Del(rbc.ctx, keys...)
		if result.Err() != nil {
			log.Printf("Failed to clear blacklist: %v", result.Err())
			return
		}
		rbc.stats.TotalEntries = 0
		log.Printf("Cleared %d blacklisted symbols", result.Val())
	}
}

// GetStats returns current cache statistics.
//
// Returns:
//
//	BlacklistCacheStats: The current statistics.
func (rbc *RedisBlacklistCache) GetStats() BlacklistCacheStats {
	rbc.mu.RLock()
	defer rbc.mu.RUnlock()

	// Update total entries count from Redis
	pattern := rbc.prefix + "*"
	var keys []string
	iter := rbc.client.Scan(rbc.ctx, 0, pattern, 0).Iterator()
	for iter.Next(rbc.ctx) {
		keys = append(keys, iter.Val())
	}
	if iter.Err() == nil {
		rbc.stats.TotalEntries = int64(len(keys))
	}

	return rbc.stats
}

// LogStats logs current cache statistics to the standard logger.
func (rbc *RedisBlacklistCache) LogStats() {
	stats := rbc.GetStats()
	log.Printf("Blacklist Cache Stats - Total: %d, Hits: %d, Misses: %d, Adds: %d, Expired: %d",
		stats.TotalEntries, stats.Hits, stats.Misses, stats.Adds, stats.ExpiredEntries)
}

// Close closes the Redis connection (if needed).
// In this implementation, the Redis client is managed externally, so this is a no-op.
//
// Returns:
//
//	error: Always nil.
func (rbc *RedisBlacklistCache) Close() error {
	// Redis client is managed externally, so we don't close it here
	return nil
}

// LoadFromDatabase loads blacklist entries from the database into Redis cache.
// This is typically called on startup to populate the cache.
//
// Parameters:
//
//	ctx: The context for the operation.
//
// Returns:
//
//	error: An error if the database retrieval or cache update fails.
func (rbc *RedisBlacklistCache) LoadFromDatabase(ctx context.Context) error {
	if rbc.repo == nil {
		return fmt.Errorf("database repository not configured")
	}

	rbc.mu.Lock()
	defer rbc.mu.Unlock()

	// Get all active blacklist entries from database
	entries, err := rbc.repo.GetAllBlacklisted(ctx)
	if err != nil {
		return fmt.Errorf("failed to load blacklist from database: %w", err)
	}

	// Load each entry into Redis cache
	for _, entry := range entries {
		// Skip expired entries
		if entry.ExpiresAt != nil && entry.ExpiresAt.Before(time.Now()) {
			continue
		}

		cacheEntry := BlacklistCacheEntry{
			Symbol:    entry.ExchangeName,
			Reason:    entry.Reason,
			CreatedAt: entry.CreatedAt,
			ExpiresAt: entry.ExpiresAt, // Use the pointer directly
		}

		data, err := json.Marshal(cacheEntry)
		if err != nil {
			log.Printf("Error marshaling blacklist entry for %s: %v", entry.ExchangeName, err)
			continue
		}

		key := rbc.prefix + entry.ExchangeName
		var ttl time.Duration
		if entry.ExpiresAt != nil {
			ttl = time.Until(*entry.ExpiresAt)
			if ttl <= 0 {
				continue // Skip expired entries
			}
		} else {
			ttl = 0 // No expiration
		}

		err = rbc.client.Set(ctx, key, data, ttl).Err()
		if err != nil {
			log.Printf("Error loading blacklist entry to cache for %s: %v", entry.ExchangeName, err)
			continue
		}
	}

	log.Printf("Loaded %d blacklist entries from database to cache", len(entries))
	return nil
}

// GetBlacklistedSymbols returns all currently blacklisted symbols.
// It scans the Redis cache for all blacklist entries.
//
// Returns:
//
//	[]BlacklistCacheEntry: A list of all blacklisted symbols.
//	error: An error if scanning fails.
func (rbc *RedisBlacklistCache) GetBlacklistedSymbols() ([]BlacklistCacheEntry, error) {
	rbc.mu.RLock()
	defer rbc.mu.RUnlock()

	pattern := rbc.prefix + "*"
	var keys []string
	iter := rbc.client.Scan(rbc.ctx, 0, pattern, 0).Iterator()
	for iter.Next(rbc.ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan blacklist keys: %w", err)
	}

	var entries []BlacklistCacheEntry
	for _, key := range keys {
		val, err := rbc.client.Get(rbc.ctx, key).Result()
		if err != nil {
			continue // Skip errors, key might have expired
		}

		var entry BlacklistCacheEntry
		if err := json.Unmarshal([]byte(val), &entry); err != nil {
			continue // Skip malformed entries
		}

		// Check if entry has expired
		if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			// Remove expired entry
			rbc.client.Del(rbc.ctx, key)
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// CleanupExpired removes all expired entries from the blacklist.
// It scans the cache and removes items where the expiration time has passed.
//
// Returns:
//
//	int: The number of expired entries removed.
func (rbc *RedisBlacklistCache) CleanupExpired() int {
	rbc.mu.Lock()
	defer rbc.mu.Unlock()

	pattern := rbc.prefix + "*"
	var keys []string
	iter := rbc.client.Scan(rbc.ctx, 0, pattern, 0).Iterator()
	for iter.Next(rbc.ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		log.Printf("Failed to scan blacklist keys for cleanup: %v", err)
		return 0
	}

	expiredCount := 0
	for _, key := range keys {
		val, err := rbc.client.Get(rbc.ctx, key).Result()
		if err != nil {
			continue
		}

		var entry BlacklistCacheEntry
		if err := json.Unmarshal([]byte(val), &entry); err != nil {
			continue
		}

		// Check if entry has expired
		if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			rbc.client.Del(rbc.ctx, key)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		rbc.stats.ExpiredEntries += int64(expiredCount)
		rbc.stats.TotalEntries -= int64(expiredCount)
		rbc.stats.LastCleanup = time.Now()
		log.Printf("Cleaned up %d expired blacklist entries", expiredCount)
	}

	return expiredCount
}

// InMemoryBlacklistCache provides a fallback in-memory implementation of BlacklistCache.
// It uses a Go map to store blacklist entries and is thread-safe.
type InMemoryBlacklistCache struct {
	cache map[string]*BlacklistCacheEntry
	mu    sync.RWMutex
	stats BlacklistCacheStats
}

// NewInMemoryBlacklistCache creates a new in-memory blacklist cache.
//
// Returns:
//
//	*InMemoryBlacklistCache: A pointer to the initialized in-memory cache.
func NewInMemoryBlacklistCache() *InMemoryBlacklistCache {
	return &InMemoryBlacklistCache{
		cache: make(map[string]*BlacklistCacheEntry),
		stats: BlacklistCacheStats{},
	}
}

// IsBlacklisted checks if a symbol is blacklisted (in-memory implementation).
//
// Parameters:
//
//	symbol: The symbol to check.
//
// Returns:
//
//	bool: True if blacklisted.
//	string: The reason for blacklisting.
func (ibc *InMemoryBlacklistCache) IsBlacklisted(symbol string) (bool, string) {
	// First, try with read lock for the common case
	ibc.mu.RLock()
	entry, exists := ibc.cache[symbol]
	if !exists {
		ibc.mu.RUnlock()
		// Need write lock to update stats
		ibc.mu.Lock()
		ibc.stats.Misses++
		ibc.mu.Unlock()
		return false, ""
	}

	// Check if entry has expired
	if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
		ibc.mu.RUnlock()
		// Need write lock to remove expired entry and update stats
		ibc.mu.Lock()
		// Double-check the entry still exists and is expired
		if entry, exists := ibc.cache[symbol]; exists && entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			delete(ibc.cache, symbol)
			ibc.stats.ExpiredEntries++
			ibc.stats.TotalEntries--
		}
		ibc.stats.Misses++
		ibc.mu.Unlock()
		return false, ""
	}

	ibc.mu.RUnlock()
	// Need write lock to update stats
	ibc.mu.Lock()
	ibc.stats.Hits++
	ibc.mu.Unlock()
	return true, entry.Reason
}

// Add adds a symbol to the blacklist (in-memory implementation).
//
// Parameters:
//
//	symbol: The symbol to blacklist.
//	reason: The reason.
//	ttl: The time-to-live.
func (ibc *InMemoryBlacklistCache) Add(symbol, reason string, ttl time.Duration) {
	ibc.mu.Lock()
	defer ibc.mu.Unlock()

	entry := &BlacklistCacheEntry{
		Symbol:    symbol,
		Reason:    reason,
		CreatedAt: time.Now(),
	}

	// Set expiration time if TTL is provided
	if ttl > 0 {
		expiresAt := time.Now().Add(ttl)
		entry.ExpiresAt = &expiresAt
	}
	// If ttl <= 0, ExpiresAt remains nil (no expiration)

	ibc.cache[symbol] = entry
	ibc.stats.Adds++
	ibc.stats.TotalEntries++
	log.Printf("Blacklisted symbol %s for %s (TTL: %v)", symbol, reason, ttl)
}

// Remove removes a symbol from the blacklist (in-memory implementation).
//
// Parameters:
//
//	symbol: The symbol to remove.
func (ibc *InMemoryBlacklistCache) Remove(symbol string) {
	ibc.mu.Lock()
	defer ibc.mu.Unlock()

	if _, exists := ibc.cache[symbol]; exists {
		delete(ibc.cache, symbol)
		ibc.stats.TotalEntries--
		log.Printf("Removed symbol %s from blacklist", symbol)
	}
}

// Clear removes all blacklisted symbols (in-memory implementation).
func (ibc *InMemoryBlacklistCache) Clear() {
	ibc.mu.Lock()
	defer ibc.mu.Unlock()

	count := len(ibc.cache)
	ibc.cache = make(map[string]*BlacklistCacheEntry)
	ibc.stats.TotalEntries = 0
	log.Printf("Cleared %d blacklisted symbols", count)
}

// GetStats returns current cache statistics (in-memory implementation).
//
// Returns:
//
//	BlacklistCacheStats: The statistics.
func (ibc *InMemoryBlacklistCache) GetStats() BlacklistCacheStats {
	ibc.mu.RLock()
	defer ibc.mu.RUnlock()
	return ibc.stats
}

// LogStats logs current cache statistics (in-memory implementation).
func (ibc *InMemoryBlacklistCache) LogStats() {
	stats := ibc.GetStats()
	log.Printf("Blacklist Cache Stats - Total: %d, Hits: %d, Misses: %d, Adds: %d, Expired: %d",
		stats.TotalEntries, stats.Hits, stats.Misses, stats.Adds, stats.ExpiredEntries)
}

// Close closes the cache (in-memory implementation).
//
// Returns:
//
//	error: Always nil.
func (ibc *InMemoryBlacklistCache) Close() error {
	return nil
}

// LoadFromDatabase loads blacklist entries from the database (in-memory implementation).
//
// Parameters:
//
//	ctx: The context.
//
// Returns:
//
//	error: Always returns error as database persistence is not supported.
func (ibc *InMemoryBlacklistCache) LoadFromDatabase(ctx context.Context) error {
	// In-memory cache doesn't support database persistence
	return fmt.Errorf("database persistence not supported for in-memory cache")
}

// GetBlacklistedSymbols returns all blacklisted symbols (in-memory implementation).
//
// Returns:
//
//	[]BlacklistCacheEntry: List of entries.
//	error: Always nil.
func (ibc *InMemoryBlacklistCache) GetBlacklistedSymbols() ([]BlacklistCacheEntry, error) {
	ibc.mu.RLock()
	defer ibc.mu.RUnlock()

	var entries []BlacklistCacheEntry
	for _, entry := range ibc.cache {
		// Check if entry has expired
		if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			continue // Skip expired entries
		}
		entries = append(entries, *entry)
	}

	return entries, nil
}
