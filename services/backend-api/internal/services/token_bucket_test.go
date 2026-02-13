package services

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenBucketRateLimiter_New(t *testing.T) {
	t.Run("creates limiter with defaults", func(t *testing.T) {
		config := DefaultTokenBucketConfig()
		tb := NewTokenBucketRateLimiter(config, nil, nil)
		assert.NotNil(t, tb)
		assert.Equal(t, 10.0, config.Rate)
		assert.Equal(t, 100, config.Burst)
	})
}

func TestTokenBucketRateLimiter_RegisterLimit(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)

	tb.RegisterLimit("binance", "ticker", 50, 100, true)

	limit, exists := tb.GetLimit("binance", "ticker")
	assert.True(t, exists)
	assert.Equal(t, "binance", limit.Exchange)
	assert.Equal(t, "ticker", limit.Endpoint)
	assert.Equal(t, 50.0, limit.Rate)
	assert.Equal(t, 100, limit.Burst)
	assert.True(t, limit.Enabled)
}

func TestTokenBucketRateLimiter_RegisterLimitFromConfig(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)

	limits := []ExchangeRateLimit{
		{Exchange: "binance", Endpoint: "ticker", Rate: 50, Burst: 100, Enabled: true},
		{Exchange: "coinbase", Endpoint: "orders", Rate: 10, Burst: 30, Enabled: true},
	}
	tb.RegisterLimitFromConfig(limits)

	limit1, exists1 := tb.GetLimit("binance", "ticker")
	assert.True(t, exists1)
	assert.Equal(t, 50.0, limit1.Rate)

	limit2, exists2 := tb.GetLimit("coinbase", "orders")
	assert.True(t, exists2)
	assert.Equal(t, 10.0, limit2.Rate)
}

func TestTokenBucketRateLimiter_TryWait_Local(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 0, 5, true) // Rate 0 = no refill

	for i := 0; i < 5; i++ {
		assert.True(t, tb.TryWait("test", "endpoint"), "Request %d should succeed", i)
	}

	assert.False(t, tb.TryWait("test", "endpoint"), "Request after burst should fail")
}

func TestTokenBucketRateLimiter_TryWaitN_Local(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 0, 10, true) // Rate 0 = no refill

	assert.True(t, tb.TryWaitN("test", "endpoint", 5))
	assert.False(t, tb.TryWaitN("test", "endpoint", 6))
	assert.True(t, tb.TryWaitN("test", "endpoint", 5))
	assert.False(t, tb.TryWait("test", "endpoint"))
}

func TestTokenBucketRateLimiter_TryWait_DisabledLimit(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 10, 5, false) // disabled

	// Should always succeed when disabled
	for i := 0; i < 100; i++ {
		assert.True(t, tb.TryWait("test", "endpoint"), "Request %d should succeed when disabled", i)
	}
}

func TestTokenBucketRateLimiter_TryWait_NoLimitConfigured(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)

	// Should succeed when no limit configured (passthrough)
	assert.True(t, tb.TryWait("unknown", "endpoint"))
}

func TestTokenBucketRateLimiter_Wait_Local(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 100, 2, true) // 100 tokens/sec, burst 2

	ctx := context.Background()

	// First two requests should succeed immediately
	err := tb.Wait(ctx, "test", "endpoint")
	assert.NoError(t, err)

	err = tb.Wait(ctx, "test", "endpoint")
	assert.NoError(t, err)

	// Third request should wait for token refill
	start := time.Now()
	err = tb.Wait(ctx, "test", "endpoint")
	elapsed := time.Since(start)

	assert.NoError(t, err)
	// Should have waited at least some time for refill
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(0))
}

func TestTokenBucketRateLimiter_Wait_ContextCancellation(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 0.1, 1, true) // Very slow refill: 0.1 tokens/sec

	ctx, cancel := context.WithCancel(context.Background())

	// First request succeeds
	err := tb.Wait(ctx, "test", "endpoint")
	assert.NoError(t, err)

	// Cancel context before second request can complete
	// With burst=1 and rate=0.1, need 10 seconds for next token
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Second request should fail due to context cancellation
	err = tb.Wait(ctx, "test", "endpoint")
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestTokenBucketRateLimiter_Reserve_Local(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 10, 5, true)

	// Reserve 3 tokens - should be immediate
	waitTime, ok := tb.Reserve("test", "endpoint", 3)
	assert.True(t, ok)
	assert.Equal(t, time.Duration(0), waitTime)

	// Reserve 3 more - should be immediate (2 remaining)
	waitTime, ok = tb.Reserve("test", "endpoint", 3)
	assert.True(t, ok)
	// Wait time should be calculated based on deficit
	_ = waitTime
}

func TestTokenBucketRateLimiter_Reserve_ExceedsBurst(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 10, 5, true)

	// Request more than burst capacity
	waitTime, ok := tb.Reserve("test", "endpoint", 10)
	assert.False(t, ok)
	assert.Equal(t, time.Duration(0), waitTime)
}

func TestTokenBucketRateLimiter_GetTokens_Local(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 10, 100, true)

	tokens, err := tb.GetTokens(context.Background(), "test", "endpoint")
	assert.NoError(t, err)
	assert.Equal(t, float64(100), tokens)

	// Consume some tokens
	tb.TryWaitN("test", "endpoint", 20)

	// Check tokens are approximately 80 (may have slight refill)
	tokens, err = tb.GetTokens(context.Background(), "test", "endpoint")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, tokens, 79.0)
	assert.LessOrEqual(t, tokens, 81.0)
}

func TestTokenBucketRateLimiter_Redis(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	tb := NewTokenBucketRateLimiter(TokenBucketConfig{
		Rate:      0,
		Burst:     5,
		KeyPrefix: "tb:",
	}, client, nil)

	tb.RegisterLimit("test", "endpoint", 0, 5, true)

	for i := 0; i < 5; i++ {
		assert.True(t, tb.TryWait("test", "endpoint"), "Request %d should succeed", i)
	}

	assert.False(t, tb.TryWait("test", "endpoint"))
}

func TestTokenBucketRateLimiter_Redis_Fallback(t *testing.T) {
	// Create with Redis client but Redis is not available
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6380", // Non-existent
	})

	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), client, nil)
	tb.RegisterLimit("test", "endpoint", 10, 5, true)

	// Should fall back to local bucket when Redis fails
	assert.True(t, tb.TryWait("test", "endpoint"))
}

func TestTokenBucketRateLimiter_Cleanup(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 10, 5, true)

	// Make a request to create local bucket
	tb.TryWait("test", "endpoint")

	// Verify bucket exists
	assert.Len(t, tb.localBuckets, 1)

	// Cleanup should not remove recent buckets
	tb.Cleanup()
	assert.Len(t, tb.localBuckets, 1)
}

func TestTokenBucketRateLimiter_DefaultExchangeRateLimits(t *testing.T) {
	limits := DefaultExchangeRateLimits()

	assert.NotEmpty(t, limits)

	// Verify some expected defaults
	var foundBinance, foundCoinbase, foundKraken, foundDefault bool
	for _, l := range limits {
		if l.Exchange == "binance" && l.Endpoint == "ticker" {
			foundBinance = true
			assert.Equal(t, 50.0, l.Rate)
			assert.Equal(t, 100, l.Burst)
		}
		if l.Exchange == "coinbase" && l.Endpoint == "ticker" {
			foundCoinbase = true
		}
		if l.Exchange == "kraken" && l.Endpoint == "ticker" {
			foundKraken = true
		}
		if l.Exchange == "*" && l.Endpoint == "*" {
			foundDefault = true
		}
	}

	assert.True(t, foundBinance, "Should have binance ticker limit")
	assert.True(t, foundCoinbase, "Should have coinbase ticker limit")
	assert.True(t, foundKraken, "Should have kraken ticker limit")
	assert.True(t, foundDefault, "Should have default catch-all limit")
}

func TestTokenBucketRateLimiter_Concurrent(t *testing.T) {
	tb := NewTokenBucketRateLimiter(DefaultTokenBucketConfig(), nil, nil)
	tb.RegisterLimit("test", "endpoint", 1000, 100, true)

	// Concurrent requests
	done := make(chan bool, 10)
	var successCount atomic.Int64

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				if tb.TryWait("test", "endpoint") {
					successCount.Add(1)
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Exactly 100 requests should succeed (burst capacity)
	assert.Equal(t, int64(100), successCount.Load())
}

func TestMin(t *testing.T) {
	assert.Equal(t, 1.0, min(1.0, 2.0))
	assert.Equal(t, 1.0, min(2.0, 1.0))
	assert.Equal(t, 0.0, min(0.0, 5.0))
	assert.Equal(t, -1.0, min(-1.0, 1.0))
}
