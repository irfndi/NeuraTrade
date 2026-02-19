// Package middleware provides HTTP middleware components for NeuraTrade.
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// RateLimitHeader is the header name for rate limit info.
	RateLimitHeader = "X-RateLimit-Limit"
	// RateLimitRemainingHeader is the header name for remaining requests.
	RateLimitRemainingHeader = "X-RateLimit-Remaining"
	// RateLimitResetHeader is the header name for reset timestamp.
	RateLimitResetHeader = "X-RateLimit-Reset"
	// RateLimitPolicyHeader is the header name for policy identifier.
	RateLimitPolicyHeader = "X-RateLimit-Policy"
)

// RateLimitConfig defines rate limit configuration.
// Controls request throttling behavior for API endpoints.
type RateLimitConfig struct {
	// Requests per window (maximum allowed requests).
	Requests int
	// Window duration for rate limiting.
	Window time.Duration
	// Key function to extract rate limit key from context.
	KeyFunc func(*gin.Context) string
	// Skip function to bypass rate limiting for certain requests.
	SkipFunc func(*gin.Context) bool
	// Alert threshold (percentage of limit) for warnings.
	AlertThreshold float64
	// Alert callback when threshold exceeded.
	OnAlert func(clientID string, usage float64)
}

// DefaultRateLimitConfig returns default rate limit configuration.
// Sets 100 requests per minute with IP-based key extraction.
//
// Returns:
//   - RateLimitConfig: Default configuration.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Requests:       100,
		Window:         time.Minute,
		AlertThreshold: 0.8,
		KeyFunc: func(c *gin.Context) string {
			// Default: use client IP
			return c.ClientIP()
		},
		SkipFunc: func(c *gin.Context) bool {
			// Skip health checks
			return c.Request.URL.Path == "/health"
		},
	}
}

// RateLimiter provides rate limiting middleware.
// Tracks request counts using Redis (with local fallback).
type RateLimiter struct {
	config RateLimitConfig
	redis  *redis.Client
	logger *zap.Logger

	// Local tracking for non-Redis fallback.
	mu       sync.RWMutex
	localMap map[string]*rateLimitEntry
}

type rateLimitEntry struct {
	count     int
	resetTime time.Time
}

// NewRateLimiter creates a new rate limiter.
//
// Parameters:
//   - config: Rate limit configuration.
//   - redisClient: Redis client for distributed rate limiting (optional).
//   - logger: Logger for rate limit events.
//
// Returns:
//   - *RateLimiter: Initialized rate limiter instance.
func NewRateLimiter(config RateLimitConfig, redisClient *redis.Client, logger *zap.Logger) *RateLimiter {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &RateLimiter{
		config:   config,
		redis:    redisClient,
		logger:   logger,
		localMap: make(map[string]*rateLimitEntry),
	}
}

// Middleware returns gin middleware for rate limiting
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if request should be skipped
		if rl.config.SkipFunc != nil && rl.config.SkipFunc(c) {
			c.Next()
			return
		}

		// Get rate limit key
		key := rl.config.KeyFunc(c)

		// Check and update rate limit
		allowed, remaining, resetTime, err := rl.checkAndUpdate(c.Request.Context(), key)
		if err != nil {
			rl.logger.Error("Rate limit check failed",
				zap.Error(err),
				zap.String("key", key),
			)
			// Fail open - allow request but log error
			c.Next()
			return
		}

		// Set rate limit headers
		c.Header(RateLimitHeader, strconv.Itoa(rl.config.Requests))
		c.Header(RateLimitRemainingHeader, strconv.Itoa(remaining))
		c.Header(RateLimitResetHeader, strconv.FormatInt(resetTime.Unix(), 10))

		if !allowed {
			c.Header(RateLimitPolicyHeader, "rate_limit_exceeded")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "Rate limit exceeded",
				"retry_after": resetTime.Unix() - time.Now().Unix(),
			})
			return
		}

		// Check alert threshold
		usage := 1.0 - (float64(remaining) / float64(rl.config.Requests))
		if usage >= rl.config.AlertThreshold && rl.config.OnAlert != nil {
			rl.config.OnAlert(key, usage)
		}

		c.Next()
	}
}

// checkAndUpdate checks rate limit and updates counter
func (rl *RateLimiter) checkAndUpdate(ctx context.Context, key string) (bool, int, time.Time, error) {
	if rl.redis != nil {
		return rl.checkAndUpdateRedis(ctx, key)
	}
	return rl.checkAndUpdateLocal(key)
}

// checkAndUpdateRedis uses Redis for distributed rate limiting
func (rl *RateLimiter) checkAndUpdateRedis(ctx context.Context, key string) (bool, int, time.Time, error) {
	redisKey := "ratelimit:" + key
	windowSeconds := int(rl.config.Window.Seconds())

	// Use Redis Lua script for atomic check and increment
	script := `
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])

		local current = redis.call("GET", key)
		if current == false then
			current = 0
		else
			current = tonumber(current)
		end

		if current >= limit then
			local ttl = redis.call("TTL", key)
			return {0, limit - current, ttl}
		end

		current = redis.call("INCR", key)
		if current == 1 then
			redis.call("EXPIRE", key, window)
		end

		local ttl = redis.call("TTL", key)
		return {1, limit - current, ttl}
	`

	result, err := rl.redis.Eval(ctx, script, []string{redisKey}, rl.config.Requests, windowSeconds).Result()
	if err != nil {
		return false, 0, time.Time{}, err
	}

	values, ok := result.([]interface{})
	if !ok || len(values) != 3 {
		return false, 0, time.Time{}, fmt.Errorf("unexpected Redis response format")
	}

	allowedVal, ok := values[0].(int64)
	if !ok {
		return false, 0, time.Time{}, fmt.Errorf("unexpected type for allowed value")
	}
	allowed := allowedVal == 1

	remainingVal, ok := values[1].(int64)
	if !ok {
		return false, 0, time.Time{}, fmt.Errorf("unexpected type for remaining value")
	}
	remaining := int(remainingVal)

	ttlVal, ok := values[2].(int64)
	if !ok {
		return false, 0, time.Time{}, fmt.Errorf("unexpected type for ttl value")
	}
	ttl := int(ttlVal)

	resetTime := time.Now().Add(time.Duration(ttl) * time.Second)

	return allowed, remaining, resetTime, nil
}

// checkAndUpdateLocal uses in-memory map for local rate limiting
func (rl *RateLimiter) checkAndUpdateLocal(key string) (bool, int, time.Time, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Periodic cleanup of expired entries (every 100 requests)
	if len(rl.localMap) > 100 {
		for k, entry := range rl.localMap {
			if now.After(entry.resetTime) {
				delete(rl.localMap, k)
			}
		}
	}

	entry, exists := rl.localMap[key]

	if !exists || now.After(entry.resetTime) {
		// Create new window
		rl.localMap[key] = &rateLimitEntry{
			count:     1,
			resetTime: now.Add(rl.config.Window),
		}
		return true, rl.config.Requests - 1, now.Add(rl.config.Window), nil
	}

	if entry.count >= rl.config.Requests {
		return false, 0, entry.resetTime, nil
	}

	entry.count++
	remaining := rl.config.Requests - entry.count

	return true, remaining, entry.resetTime, nil
}

// GetStats returns current rate limit statistics for a key
func (rl *RateLimiter) GetStats(ctx context.Context, key string) (*RateLimitStats, error) {
	if rl.redis != nil {
		redisKey := "ratelimit:" + key
		count, err := rl.redis.Get(ctx, redisKey).Int()
		if err == redis.Nil {
			return &RateLimitStats{
				Key:       key,
				Limit:     rl.config.Requests,
				Remaining: rl.config.Requests,
				Window:    rl.config.Window,
			}, nil
		}
		if err != nil {
			return nil, err
		}

		ttl, err := rl.redis.TTL(ctx, redisKey).Result()
		if err != nil {
			return nil, err
		}

		return &RateLimitStats{
			Key:       key,
			Limit:     rl.config.Requests,
			Used:      count,
			Remaining: rl.config.Requests - count,
			Window:    rl.config.Window,
			ResetTime: time.Now().Add(ttl),
		}, nil
	}

	// Local mode
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	entry, exists := rl.localMap[key]
	if !exists || time.Now().After(entry.resetTime) {
		return &RateLimitStats{
			Key:       key,
			Limit:     rl.config.Requests,
			Remaining: rl.config.Requests,
			Window:    rl.config.Window,
		}, nil
	}

	return &RateLimitStats{
		Key:       key,
		Limit:     rl.config.Requests,
		Used:      entry.count,
		Remaining: rl.config.Requests - entry.count,
		Window:    rl.config.Window,
		ResetTime: entry.resetTime,
	}, nil
}

// RateLimitStats represents rate limit statistics
type RateLimitStats struct {
	Key       string        `json:"key"`
	Limit     int           `json:"limit"`
	Used      int           `json:"used,omitempty"`
	Remaining int           `json:"remaining"`
	Window    time.Duration `json:"window"`
	ResetTime time.Time     `json:"reset_time,omitempty"`
}

// Reset resets the rate limit for a key
func (rl *RateLimiter) Reset(ctx context.Context, key string) error {
	if rl.redis != nil {
		redisKey := "ratelimit:" + key
		return rl.redis.Del(ctx, redisKey).Err()
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.localMap, key)
	return nil
}

// Cleanup removes expired entries from local map
func (rl *RateLimiter) Cleanup() {
	if rl.redis != nil {
		return // Redis handles expiration automatically
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, entry := range rl.localMap {
		if now.After(entry.resetTime) {
			delete(rl.localMap, key)
		}
	}
}
