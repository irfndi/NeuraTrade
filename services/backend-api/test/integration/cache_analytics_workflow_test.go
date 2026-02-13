package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/irfndi/neuratrade/internal/services"
)

func TestIntegrationCacheAnalyticsReporting(t *testing.T) {
	redisServer := miniredis.RunT(t)

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisServer.Addr(),
	})

	analytics := services.NewCacheAnalyticsService(redisClient)
	analytics.RecordHit("orders")
	analytics.RecordMiss("orders")

	reportCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	analytics.StartPeriodicReporting(reportCtx, 10*time.Millisecond)

	assert.Eventually(t, func() bool {
		value, err := redisClient.Get(context.Background(), "cache:analytics:stats").Result()
		return err == nil && value != ""
	}, 300*time.Millisecond, 10*time.Millisecond)

	timeoutManager := services.NewTimeoutManager(nil, zaplogrus.New())
	result, err := timeoutManager.ExecuteWithTimeout("redis_operation", "cache_metrics", func(ctx context.Context) (interface{}, error) {
		return analytics.GetMetrics(ctx)
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	metrics, ok := result.(*services.CacheMetrics)
	require.True(t, ok)
	assert.Equal(t, int64(1), metrics.Overall.Hits)
	assert.Equal(t, int64(1), metrics.Overall.Misses)
	assert.Equal(t, int64(2), metrics.Overall.TotalOps)
}
