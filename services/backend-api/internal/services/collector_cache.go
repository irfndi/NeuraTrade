package services

import (
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/logging"
)

// NewSymbolCache creates a new symbol cache with specified TTL.
//
// Parameters:
//
//	ttl: Time to live for cache entries.
//
// Returns:
//
//	*SymbolCache: Initialized cache.
func NewSymbolCache(ttl time.Duration, logger logging.Logger) *SymbolCache {
	return &SymbolCache{
		cache:  make(map[string]*SymbolCacheEntry),
		ttl:    ttl,
		logger: logger,
	}
}

// Get retrieves symbols from cache if not expired.
//
// Parameters:
//
//	exchangeID: Exchange identifier.
//
// Returns:
//
//	[]string: List of symbols.
//	bool: True if found and valid.
func (sc *SymbolCache) Get(exchangeID string) ([]string, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	entry, exists := sc.cache[exchangeID]
	if !exists {
		sc.stats.Misses++
		sc.logger.Debug("Cache MISS", "exchange", exchangeID)
		return nil, false
	}

	// Check if entry has expired
	if time.Now().After(entry.ExpiresAt) {
		// Entry expired, treat as cache miss
		sc.stats.Misses++
		sc.logger.Debug("Cache MISS (expired)", "exchange", exchangeID)
		return nil, false
	}

	sc.stats.Hits++
	sc.logger.Debug("Cache HIT", "exchange", exchangeID, "symbols", len(entry.Symbols))

	return entry.Symbols, true
}

// Set stores symbols in cache with TTL.
//
// Parameters:
//
//	exchangeID: Exchange identifier.
//
//	symbols: List of symbols to cache.
func (sc *SymbolCache) Set(exchangeID string, symbols []string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.stats.Sets++
	sc.cache[exchangeID] = &SymbolCacheEntry{
		Symbols:   symbols,
		ExpiresAt: time.Now().Add(sc.ttl),
	}
	sc.logger.Debug("Cache SET", "exchange", exchangeID, "symbols", len(symbols))
}

// GetStats returns current cache statistics.
//
// Returns:
//
//	cache.SymbolCacheStats: Cache statistics.
func (sc *SymbolCache) GetStats() cache.SymbolCacheStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return cache.SymbolCacheStats{
		Hits:   sc.stats.Hits,
		Misses: sc.stats.Misses,
		Sets:   sc.stats.Sets,
	}
}

// LogStats logs current cache performance statistics.
func (sc *SymbolCache) LogStats() {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	total := sc.stats.Hits + sc.stats.Misses
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(sc.stats.Hits) / float64(total) * 100
	}

	sc.logger.WithFields(map[string]interface{}{
		"hits":             sc.stats.Hits,
		"misses":           sc.stats.Misses,
		"sets":             sc.stats.Sets,
		"hit_rate_percent": hitRate,
	}).Info("Symbol Cache Stats")
}

// NewExchangeCapabilityCache creates a new exchange capability cache with specified TTL.
//
// Parameters:
//
//	ttl: Time to live.
//
// Returns:
//
//	*ExchangeCapabilityCache: Initialized cache.
func NewExchangeCapabilityCache(ttl time.Duration, logger logging.Logger) *ExchangeCapabilityCache {
	return &ExchangeCapabilityCache{
		cache:  make(map[string]*ExchangeCapabilityEntry),
		ttl:    ttl,
		logger: logger,
	}
}

// SupportsFundingRates checks if an exchange supports funding rates.
//
// Parameters:
//
//	exchange: Exchange name.
//
// Returns:
//
//	bool: True if supported.
//	bool: True if info is found in cache.
func (ecc *ExchangeCapabilityCache) SupportsFundingRates(exchange string) (bool, bool) {
	ecc.mu.RLock()
	defer ecc.mu.RUnlock()

	entry, exists := ecc.cache[exchange]
	if !exists {
		return false, false // unknown capability
	}

	// Check if entry has expired
	if time.Now().After(entry.ExpiresAt) {
		return false, false // expired, need to recheck
	}

	return entry.SupportsFundingRates, true // known capability
}

// SetFundingRateSupport sets the funding rate support capability for an exchange.
//
// Parameters:
//
//	exchange: Exchange name.
//
//	supports: Whether funding rates are supported.
func (ecc *ExchangeCapabilityCache) SetFundingRateSupport(exchange string, supports bool) {
	ecc.mu.Lock()
	defer ecc.mu.Unlock()

	ecc.cache[exchange] = &ExchangeCapabilityEntry{
		SupportsFundingRates: supports,
		LastChecked:          time.Now(),
		ExpiresAt:            time.Now().Add(ecc.ttl),
	}
	ecc.logger.WithFields(map[string]interface{}{
		"exchange":               exchange,
		"supports_funding_rates": supports,
	}).Debug("Exchange capability cached")
}

// initializeSymbolCache creates either Redis-based or in-memory symbol cache
func initializeSymbolCache(redisClient *redis.Client, logger logging.Logger) SymbolCacheInterface {
	if redisClient != nil {
		logger.Info("Initializing Redis-based symbol cache")
		return cache.NewRedisSymbolCache(redisClient, 1*time.Hour)
	}
	logger.Info("Redis client not available, using in-memory symbol cache")
	return NewSymbolCache(1*time.Hour, logger)
}
