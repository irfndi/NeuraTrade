package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// TokenBucketConfig defines configuration for a token bucket rate limiter.
type TokenBucketConfig struct {
	// Rate is the number of tokens added per second (refill rate)
	Rate float64
	// Burst is the maximum bucket capacity (max tokens)
	Burst int
	// KeyPrefix for Redis storage
	KeyPrefix string
}

// DefaultTokenBucketConfig returns a sensible default configuration.
func DefaultTokenBucketConfig() TokenBucketConfig {
	return TokenBucketConfig{
		Rate:      10.0,  // 10 requests per second
		Burst:     100,   // Allow burst of 100 requests
		KeyPrefix: "tb:", // Token bucket prefix
	}
}

// ExchangeRateLimit defines rate limits for a specific exchange and endpoint.
type ExchangeRateLimit struct {
	Exchange string
	Endpoint string
	Rate     float64 // Tokens per second
	Burst    int     // Maximum burst
	Enabled  bool    // Whether rate limiting is enabled for this endpoint
}

// TokenBucketRateLimiter implements the token bucket algorithm for API rate limiting.
// Supports per-exchange and per-endpoint rate limits with Redis-backed distributed state.
type TokenBucketRateLimiter struct {
	redis        *redis.Client
	logger       *zap.Logger
	mu           sync.RWMutex
	config       TokenBucketConfig
	limits       map[string]*ExchangeRateLimit // key: "exchange:endpoint"
	localBuckets map[string]*localTokenBucket
}

// localTokenBucket is an in-memory fallback when Redis is unavailable.
type localTokenBucket struct {
	tokens     float64
	lastUpdate time.Time
	rate       float64
	burst      int
	mu         sync.Mutex
}

// NewTokenBucketRateLimiter creates a new token bucket rate limiter.
func NewTokenBucketRateLimiter(config TokenBucketConfig, redisClient *redis.Client, logger *zap.Logger) *TokenBucketRateLimiter {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &TokenBucketRateLimiter{
		redis:        redisClient,
		logger:       logger,
		config:       config,
		limits:       make(map[string]*ExchangeRateLimit),
		localBuckets: make(map[string]*localTokenBucket),
	}
}

// RegisterLimit registers a rate limit for a specific exchange and endpoint.
func (tb *TokenBucketRateLimiter) RegisterLimit(exchange, endpoint string, rate float64, burst int, enabled bool) {
	key := tb.limitKey(exchange, endpoint)
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.limits[key] = &ExchangeRateLimit{
		Exchange: exchange,
		Endpoint: endpoint,
		Rate:     rate,
		Burst:    burst,
		Enabled:  enabled,
	}
}

// RegisterLimitFromConfig registers multiple rate limits from a configuration map.
func (tb *TokenBucketRateLimiter) RegisterLimitFromConfig(limits []ExchangeRateLimit) {
	for _, limit := range limits {
		tb.RegisterLimit(limit.Exchange, limit.Endpoint, limit.Rate, limit.Burst, limit.Enabled)
	}
}

// GetLimit retrieves the rate limit for a specific exchange and endpoint.
func (tb *TokenBucketRateLimiter) GetLimit(exchange, endpoint string) (*ExchangeRateLimit, bool) {
	key := tb.limitKey(exchange, endpoint)
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	limit, exists := tb.limits[key]
	return limit, exists
}

// Wait blocks until a token is available for the given exchange and endpoint.
// Returns an error if the context is cancelled before a token is available.
func (tb *TokenBucketRateLimiter) Wait(ctx context.Context, exchange, endpoint string) error {
	return tb.WaitN(ctx, exchange, endpoint, 1)
}

// WaitN blocks until n tokens are available for the given exchange and endpoint.
func (tb *TokenBucketRateLimiter) WaitN(ctx context.Context, exchange, endpoint string, n int) error {
	key := tb.limitKey(exchange, endpoint)

	// Check if rate limiting is enabled for this endpoint
	tb.mu.RLock()
	limit, exists := tb.limits[key]
	tb.mu.RUnlock()

	if !exists || !limit.Enabled {
		// No rate limit configured or disabled - allow immediately
		return nil
	}

	// Validate request size
	if n > limit.Burst {
		return fmt.Errorf("request size %d exceeds burst capacity %d", n, limit.Burst)
	}

	// Try Redis first, fall back to local
	if tb.redis != nil {
		return tb.waitRedis(ctx, key, n, limit.Rate, limit.Burst)
	}
	return tb.waitLocal(ctx, key, n, limit.Rate, limit.Burst)
}

// TryWait attempts to acquire a token without blocking.
// Returns true if successful, false if rate limited.
func (tb *TokenBucketRateLimiter) TryWait(exchange, endpoint string) bool {
	return tb.TryWaitN(exchange, endpoint, 1)
}

// TryWaitN attempts to acquire n tokens without blocking.
func (tb *TokenBucketRateLimiter) TryWaitN(exchange, endpoint string, n int) bool {
	key := tb.limitKey(exchange, endpoint)

	tb.mu.RLock()
	limit, exists := tb.limits[key]
	tb.mu.RUnlock()

	if !exists || !limit.Enabled {
		return true
	}

	if n > limit.Burst {
		return false
	}

	if tb.redis != nil {
		return tb.tryWaitRedis(key, n, limit.Rate, limit.Burst)
	}
	return tb.tryWaitLocal(key, n, limit.Rate, limit.Burst)
}

// Reserve reserves tokens and returns the time to wait before using them.
func (tb *TokenBucketRateLimiter) Reserve(exchange, endpoint string, n int) (time.Duration, bool) {
	key := tb.limitKey(exchange, endpoint)

	tb.mu.RLock()
	limit, exists := tb.limits[key]
	tb.mu.RUnlock()

	if !exists || !limit.Enabled {
		return 0, true
	}

	if n > limit.Burst {
		return 0, false
	}

	if tb.redis != nil {
		return tb.reserveRedis(key, n, limit.Rate, limit.Burst)
	}
	return tb.reserveLocal(key, n, limit.Rate, limit.Burst), true
}

// GetTokens returns the current number of tokens available for the given key.
func (tb *TokenBucketRateLimiter) GetTokens(ctx context.Context, exchange, endpoint string) (float64, error) {
	key := tb.limitKey(exchange, endpoint)

	tb.mu.RLock()
	limit, exists := tb.limits[key]
	tb.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("no rate limit configured for %s", key)
	}

	if tb.redis != nil {
		return tb.getTokensRedis(ctx, key, limit.Rate, limit.Burst)
	}
	return tb.getTokensLocal(key, limit.Rate, limit.Burst), nil
}

// Redis-based token bucket operations

func (tb *TokenBucketRateLimiter) waitRedis(ctx context.Context, key string, n int, rate float64, burst int) error {
	// Calculate wait time using Redis Lua script
	for {
		waitTime, ok := tb.reserveRedis(key, n, rate, burst)
		if ok && waitTime == 0 {
			return nil
		}

		if !ok {
			return fmt.Errorf("cannot reserve %d tokens (exceeds burst)", n)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Try again after waiting
			continue
		}
	}
}

func (tb *TokenBucketRateLimiter) tryWaitRedis(key string, n int, rate float64, burst int) bool {
	waitTime, ok := tb.reserveRedis(key, n, rate, burst)
	return ok && waitTime == 0
}

func (tb *TokenBucketRateLimiter) reserveRedis(key string, n int, rate float64, burst int) (time.Duration, bool) {
	ctx := context.Background()
	redisKey := tb.config.KeyPrefix + key

	// Token bucket Lua script
	// Returns: {tokens_remaining, wait_time_seconds}
	// tokens_remaining >= 0 means success, -1 means would exceed burst
	script := `
		local key = KEYS[1]
		local rate = tonumber(ARGV[1])
		local burst = tonumber(ARGV[2])
		local n = tonumber(ARGV[3])
		local now = tonumber(ARGV[4])

		-- Get current state
		local state = redis.call("HMGET", key, "tokens", "last_update")
		local tokens = tonumber(state[1])
		local lastUpdate = tonumber(state[2])

		if tokens == nil then
			tokens = burst
			lastUpdate = now
		end

		-- Calculate tokens added since last update
		local elapsed = now - lastUpdate
		local newTokens = elapsed * rate
		tokens = math.min(burst, tokens + newTokens)

		-- Check if we have enough tokens
		if tokens < n then
			-- Calculate wait time (handle rate=0 to avoid division by zero)
			local deficit = n - tokens
			local waitTime
			if rate <= 0 then
				waitTime = 3153600000  -- 100 years in seconds (tokens will never be available)
			else
				waitTime = deficit / rate
			end
			return {tokens, waitTime}
		end

		-- Consume tokens
		tokens = tokens - n

		-- Update state
		redis.call("HMSET", key, "tokens", tokens, "last_update", now)
		redis.call("EXPIRE", key, 3600) -- 1 hour TTL

		return {tokens, 0}
	`

	now := float64(time.Now().UnixNano()) / 1e9 // Seconds with nanosecond precision

	result, err := tb.redis.Eval(ctx, script, []string{redisKey}, rate, burst, n, now).Result()
	if err != nil {
		tb.logger.Error("Redis token bucket error", zap.Error(err), zap.String("key", key))
		// Fall back to local bucket on Redis error
		return tb.reserveLocal(key, n, rate, burst), true
	}

	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return 0, false
	}

	// Handle both int64 and float64 from Lua script
	var tokensRemaining int64
	var waitTimeSecs float64

	switch v := values[0].(type) {
	case int64:
		tokensRemaining = v
	case float64:
		tokensRemaining = int64(v)
	}

	switch v := values[1].(type) {
	case int64:
		waitTimeSecs = float64(v)
	case float64:
		waitTimeSecs = v
	}

	if waitTimeSecs > 0 {
		return time.Duration(waitTimeSecs * float64(time.Second)), true
	}

	_ = tokensRemaining
	return 0, true
}

func (tb *TokenBucketRateLimiter) getTokensRedis(ctx context.Context, key string, rate float64, burst int) (float64, error) {
	redisKey := tb.config.KeyPrefix + key

	// Get current state and calculate available tokens
	state, err := tb.redis.HMGet(ctx, redisKey, "tokens", "last_update").Result()
	if err != nil {
		return 0, err
	}

	if state[0] == nil {
		return float64(burst), nil
	}

	tokens, _ := state[0].(string)
	lastUpdate, _ := state[1].(string)

	if tokens == "" {
		return float64(burst), nil
	}

	// Parse and calculate current tokens
	var currentTokens float64
	var lastUpdateSec float64
	fmt.Sscanf(tokens, "%f", &currentTokens)
	fmt.Sscanf(lastUpdate, "%f", &lastUpdateSec)

	now := float64(time.Now().UnixNano()) / 1e9
	elapsed := now - lastUpdateSec
	newTokens := elapsed * rate
	currentTokens = min(float64(burst), currentTokens+newTokens)

	return currentTokens, nil
}

// Local (in-memory) token bucket operations

func (tb *TokenBucketRateLimiter) waitLocal(ctx context.Context, key string, n int, rate float64, burst int) error {
	for {
		waitTime := tb.reserveLocal(key, n, rate, burst)
		if waitTime == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			continue
		}
	}
}

func (tb *TokenBucketRateLimiter) tryWaitLocal(key string, n int, rate float64, burst int) bool {
	return tb.reserveLocal(key, n, rate, burst) == 0
}

func (tb *TokenBucketRateLimiter) reserveLocal(key string, n int, rate float64, burst int) time.Duration {
	tb.mu.Lock()
	bucket, exists := tb.localBuckets[key]
	if !exists {
		bucket = &localTokenBucket{
			tokens:     float64(burst),
			lastUpdate: time.Now(),
			rate:       rate,
			burst:      burst,
		}
		tb.localBuckets[key] = bucket
	}
	tb.mu.Unlock()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(bucket.lastUpdate).Seconds()
	bucket.tokens = min(float64(bucket.burst), bucket.tokens+elapsed*bucket.rate)
	bucket.lastUpdate = now

	if bucket.tokens >= float64(n) {
		bucket.tokens -= float64(n)
		return 0
	}

	// Calculate wait time
	deficit := float64(n) - bucket.tokens
	if rate <= 0 {
		// No refill - return max duration to indicate tokens will never be available
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(deficit/rate) * time.Second
}

func (tb *TokenBucketRateLimiter) getTokensLocal(key string, rate float64, burst int) float64 {
	tb.mu.RLock()
	bucket, exists := tb.localBuckets[key]
	tb.mu.RUnlock()

	if !exists {
		return float64(burst)
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(bucket.lastUpdate).Seconds()
	return min(float64(bucket.burst), bucket.tokens+elapsed*bucket.rate)
}

// Helper functions

func (tb *TokenBucketRateLimiter) limitKey(exchange, endpoint string) string {
	return fmt.Sprintf("%s:%s", exchange, endpoint)
}

// Cleanup removes stale local buckets.
func (tb *TokenBucketRateLimiter) Cleanup() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Remove buckets that haven't been used in over an hour
	cutoff := time.Now().Add(-time.Hour)
	for key, bucket := range tb.localBuckets {
		bucket.mu.Lock()
		if bucket.lastUpdate.Before(cutoff) {
			delete(tb.localBuckets, key)
		}
		bucket.mu.Unlock()
	}
}

// DefaultExchangeRateLimits returns sensible defaults for common exchanges.
func DefaultExchangeRateLimits() []ExchangeRateLimit {
	return []ExchangeRateLimit{
		// Binance
		{Exchange: "binance", Endpoint: "ticker", Rate: 50, Burst: 100, Enabled: true},
		{Exchange: "binance", Endpoint: "orderbook", Rate: 50, Burst: 100, Enabled: true},
		{Exchange: "binance", Endpoint: "orders", Rate: 50, Burst: 100, Enabled: true},
		{Exchange: "binance", Endpoint: "account", Rate: 10, Burst: 20, Enabled: true},

		// Coinbase
		{Exchange: "coinbase", Endpoint: "ticker", Rate: 10, Burst: 30, Enabled: true},
		{Exchange: "coinbase", Endpoint: "orderbook", Rate: 10, Burst: 30, Enabled: true},
		{Exchange: "coinbase", Endpoint: "orders", Rate: 10, Burst: 25, Enabled: true},

		// Kraken
		{Exchange: "kraken", Endpoint: "ticker", Rate: 20, Burst: 50, Enabled: true},
		{Exchange: "kraken", Endpoint: "orderbook", Rate: 20, Burst: 50, Enabled: true},
		{Exchange: "kraken", Endpoint: "orders", Rate: 10, Burst: 30, Enabled: true},

		// Default for unknown exchanges
		{Exchange: "*", Endpoint: "*", Rate: 10, Burst: 30, Enabled: true},
	}
}
