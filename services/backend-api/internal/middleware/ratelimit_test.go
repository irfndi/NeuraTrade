package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultRateLimitConfig(t *testing.T) {
	config := DefaultRateLimitConfig()

	assert.Equal(t, 100, config.Requests)
	assert.Equal(t, time.Minute, config.Window)
	assert.Equal(t, 0.8, config.AlertThreshold)
	assert.NotNil(t, config.KeyFunc)
	assert.NotNil(t, config.SkipFunc)

	// Test KeyFunc
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

	key := config.KeyFunc(c)
	assert.NotEmpty(t, key)

	// Test SkipFunc
	c2, _ := gin.CreateTestContext(w)
	c2.Request = httptest.NewRequest(http.MethodGet, "/health", nil)
	assert.True(t, config.SkipFunc(c2))

	c3, _ := gin.CreateTestContext(w)
	c3.Request = httptest.NewRequest(http.MethodGet, "/api/test", nil)
	assert.False(t, config.SkipFunc(c3))
}

func TestNewRateLimiter(t *testing.T) {
	config := DefaultRateLimitConfig()

	rl := NewRateLimiter(config, nil, nil)
	assert.NotNil(t, rl)
	assert.NotNil(t, rl.localMap)
	assert.NotNil(t, rl.logger)

	// With logger
	logger := zap.NewNop()
	rl2 := NewRateLimiter(config, nil, logger)
	assert.NotNil(t, rl2)
}

func TestRateLimiterMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	config := RateLimitConfig{
		Requests:       2,
		Window:         time.Minute,
		AlertThreshold: 0.5,
		KeyFunc: func(c *gin.Context) string {
			return "test-client"
		},
		SkipFunc: func(c *gin.Context) bool {
			return false
		},
		OnAlert: func(clientID string, usage float64) {
			// Alert callback
		},
	}

	rl := NewRateLimiter(config, client, zap.NewNop())

	router := gin.New()
	router.Use(rl.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	t.Run("allows requests within limit", func(t *testing.T) {
		ctx := context.Background()
		rl.Reset(ctx, "test-client")

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, w.Header().Get(RateLimitHeader))
		assert.NotEmpty(t, w.Header().Get(RateLimitRemainingHeader))
	})

	t.Run("blocks requests exceeding limit", func(t *testing.T) {
		ctx := context.Background()
		rl.Reset(ctx, "test-client")

		// Make requests up to limit
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// Next request should be blocked
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("skips when SkipFunc returns true", func(t *testing.T) {
		skipConfig := RateLimitConfig{
			Requests: 1,
			Window:   time.Minute,
			KeyFunc: func(c *gin.Context) string {
				return "skip-test"
			},
			SkipFunc: func(c *gin.Context) bool {
				return c.Request.URL.Path == "/skip"
			},
		}

		skipRl := NewRateLimiter(skipConfig, nil, zap.NewNop())

		skipRouter := gin.New()
		skipRouter.Use(skipRl.Middleware())
		skipRouter.GET("/skip", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Make multiple requests - should not be rate limited
		for i := 0; i < 5; i++ {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/skip", nil)
			skipRouter.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})
}

func TestCheckAndUpdateLocal(t *testing.T) {
	config := RateLimitConfig{
		Requests: 3,
		Window:   time.Minute,
	}

	rl := NewRateLimiter(config, nil, zap.NewNop())
	key := "test-local-key"

	// First request
	allowed, remaining, resetTime, err := rl.checkAndUpdateLocal(key)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 2, remaining)
	assert.False(t, resetTime.IsZero())

	// Second request
	allowed, remaining, _, err = rl.checkAndUpdateLocal(key)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 1, remaining)

	// Third request
	allowed, remaining, _, err = rl.checkAndUpdateLocal(key)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 0, remaining)

	// Fourth request - should be blocked
	allowed, remaining, _, err = rl.checkAndUpdateLocal(key)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
}

func TestGetStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer client.Close()

	config := RateLimitConfig{
		Requests: 10,
		Window:   time.Minute,
	}

	rl := NewRateLimiter(config, client, zap.NewNop())

	t.Run("get stats with Redis", func(t *testing.T) {
		key := "stats-test"
		ctx := context.Background()

		stats, err := rl.GetStats(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, key, stats.Key)
		assert.Equal(t, 10, stats.Limit)
		assert.Equal(t, 10, stats.Remaining)

		for i := 0; i < 3; i++ {
			_, _, _, err := rl.checkAndUpdate(ctx, key)
			require.NoError(t, err)
		}

		stats, err = rl.GetStats(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, 3, stats.Used)
		assert.Equal(t, 7, stats.Remaining)
	})

	t.Run("get stats local mode", func(t *testing.T) {
		localRl := NewRateLimiter(config, nil, zap.NewNop())
		key := "local-stats-test"

		stats, err := localRl.GetStats(nil, key)
		require.NoError(t, err)
		assert.Equal(t, 10, stats.Remaining)

		_, _, _, err = localRl.checkAndUpdate(nil, key)
		require.NoError(t, err)

		stats, err = localRl.GetStats(nil, key)
		require.NoError(t, err)
		assert.Equal(t, 1, stats.Used)
		assert.Equal(t, 9, stats.Remaining)
	})
}

func TestReset(t *testing.T) {
	config := RateLimitConfig{
		Requests: 2,
		Window:   time.Minute,
	}

	rl := NewRateLimiter(config, nil, zap.NewNop())
	key := "reset-test"

	// Use up the limit
	_, _, _, _ = rl.checkAndUpdateLocal(key)
	_, _, _, _ = rl.checkAndUpdateLocal(key)

	// Should be blocked
	allowed, _, _, _ := rl.checkAndUpdateLocal(key)
	assert.False(t, allowed)

	// Reset
	err := rl.Reset(context.Background(), key)
	require.NoError(t, err)

	allowed, remaining, _, err := rl.checkAndUpdateLocal(key)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 1, remaining)
}

func TestCleanup(t *testing.T) {
	config := RateLimitConfig{
		Requests: 10,
		Window:   100 * time.Millisecond,
	}

	rl := NewRateLimiter(config, nil, zap.NewNop())

	// Add entries
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		_, _, _, _ = rl.checkAndUpdateLocal(key)
	}

	assert.Len(t, rl.localMap, 5)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Cleanup
	rl.Cleanup()

	assert.Empty(t, rl.localMap)
}

func TestRateLimitHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := DefaultRateLimitConfig()
	rl := NewRateLimiter(config, nil, zap.NewNop())

	router := gin.New()
	router.Use(rl.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get(RateLimitHeader))
	assert.NotEmpty(t, w.Header().Get(RateLimitRemainingHeader))
	assert.NotEmpty(t, w.Header().Get(RateLimitResetHeader))
}
