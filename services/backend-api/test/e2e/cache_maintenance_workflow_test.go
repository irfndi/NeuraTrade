package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/irfandi/celebrum-ai-go/internal/ccxt"
	"github.com/irfandi/celebrum-ai-go/internal/services"
)

type stubCCXTService struct {
	ccxt.CCXTService
}

func (s stubCCXTService) GetExchangeConfig(ctx context.Context) (*ccxt.ExchangeConfigResponse, error) {
	return &ccxt.ExchangeConfigResponse{}, nil
}

func (s stubCCXTService) GetSupportedExchanges() []string {
	return []string{"binance"}
}

func TestE2ECacheMaintenanceWorkflow(t *testing.T) {
	redisServer := miniredis.RunT(t)

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisServer.Addr(),
	})

	warmingService := services.NewCacheWarmingService(redisClient, stubCCXTService{}, nil)
	err := warmingService.WarmCache(context.Background())
	assert.NoError(t, err)

	configValue, err := redisClient.Get(context.Background(), "exchange:config").Result()
	require.NoError(t, err)
	assert.NotEmpty(t, configValue)

	supportedValue, err := redisClient.Get(context.Background(), "exchange:supported").Result()
	require.NoError(t, err)
	assert.NotEmpty(t, supportedValue)

	analytics := services.NewCacheAnalyticsService(redisClient)
	analytics.RecordHit("trading_pairs")
	analytics.RecordMiss("trading_pairs")

	reportCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	analytics.StartPeriodicReporting(reportCtx, 10*time.Millisecond)

	assert.Eventually(t, func() bool {
		value, err := redisClient.Get(context.Background(), "cache:analytics:stats").Result()
		return err == nil && value != ""
	}, 300*time.Millisecond, 10*time.Millisecond)
}
