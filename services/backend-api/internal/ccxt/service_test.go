package ccxt

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/config"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)
	require.NotNil(t, service)
	assert.NotNil(t, service.client)
	assert.NotNil(t, service.supportedExchanges)
}

func TestService_Initialize(t *testing.T) {
	// This test requires a running CCXT service
	t.Skip("Skipping integration test - requires running CCXT service")

	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := service.Initialize(ctx)
	assert.NoError(t, err)

	exchanges := service.GetSupportedExchanges()
	assert.NotEmpty(t, exchanges)
}

func TestService_GetSupportedExchanges(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Before initialization, should return empty slice
	exchanges := service.GetSupportedExchanges()
	assert.Empty(t, exchanges)
}

func TestService_FetchSingleTicker(t *testing.T) {
	// This test requires a running CCXT service
	t.Skip("Skipping integration test - requires running CCXT service")

	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Initialize service first
	err := service.Initialize(ctx)
	require.NoError(t, err)

	// Test fetching ticker
	marketPrice, err := service.FetchSingleTicker(ctx, "binance", "BTC/USDT")
	assert.NoError(t, err)
	assert.NotNil(t, marketPrice)

	mp, ok := marketPrice.(*models.MarketPrice)
	require.True(t, ok)
	assert.Equal(t, "binance", mp.ExchangeName)
	assert.Equal(t, "BTC/USDT", mp.Symbol)
	assert.True(t, mp.Price.IsPositive())
}

func TestService_FetchMarketData(t *testing.T) {
	// This test requires a running CCXT service
	t.Skip("Skipping integration test - requires running CCXT service")

	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Initialize service first
	err := service.Initialize(ctx)
	require.NoError(t, err)

	// Test fetching market data
	exchanges := []string{"binance", "bybit"}
	symbols := []string{"BTC/USDT", "ETH/USDT"}

	marketData, err := service.FetchMarketData(ctx, exchanges, symbols)
	assert.NoError(t, err)
	assert.NotEmpty(t, marketData)

	// Should have data for each exchange-symbol combination
	for _, data := range marketData {
		mp, ok := data.(*models.MarketPrice)
		require.True(t, ok)
		assert.Contains(t, exchanges, mp.ExchangeName)
		assert.Contains(t, symbols, mp.Symbol)
		assert.True(t, mp.Price.IsPositive())
	}
}

// Test FetchMarketData with empty parameters
func TestService_FetchMarketData_EmptyExchanges(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)
	ctx := context.Background()

	// Test with empty exchanges
	data, err := service.FetchMarketData(ctx, []string{}, []string{"BTC/USDT"})

	assert.Error(t, err)
	assert.Nil(t, data)
	assert.Contains(t, err.Error(), "exchanges and symbols cannot be empty")
}

func TestService_FetchMarketData_EmptySymbols(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)
	ctx := context.Background()

	// Test with empty symbols
	data, err := service.FetchMarketData(ctx, []string{"binance"}, []string{})

	assert.Error(t, err)
	assert.Nil(t, data)
	assert.Contains(t, err.Error(), "exchanges and symbols cannot be empty")
}

// Test GetExchangeInfo with existing exchange
func TestService_GetExchangeInfo_Exists(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Manually populate supported exchanges for testing
	binanceInfo := ExchangeInfo{
		ID:        "binance",
		Name:      "Binance",
		Countries: []string{"MT"},
	}

	service.mu.Lock()
	service.supportedExchanges["binance"] = binanceInfo
	service.mu.Unlock()

	info, exists := service.GetExchangeInfo("binance")

	assert.True(t, exists)
	assert.Equal(t, binanceInfo.ID, info.ID)
	assert.Equal(t, binanceInfo.Name, info.Name)
	assert.Equal(t, binanceInfo.Countries, info.Countries)
}

// Test GetExchangeInfo with non-existing exchange
func TestService_GetExchangeInfo_NotExists(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	info, exists := service.GetExchangeInfo("nonexistent")

	assert.False(t, exists)
	assert.Empty(t, info.ID)
	assert.Empty(t, info.Name)
}

// Test Service mutex operations
func TestService_ConcurrentAccess(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Test concurrent read/write operations
	done := make(chan bool, 2)

	// Writer goroutine
	go func() {
		service.mu.Lock()
		service.supportedExchanges["test"] = ExchangeInfo{ID: "test", Name: "Test"}
		service.lastUpdate = time.Now()
		service.mu.Unlock()
		done <- true
	}()

	// Reader goroutine
	go func() {
		exchanges := service.GetSupportedExchanges()
		_ = exchanges // Use the result to avoid unused variable
		done <- true
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// Verify the exchange was added
	info, exists := service.GetExchangeInfo("test")
	assert.True(t, exists)
	assert.Equal(t, "test", info.ID)
}

// Test Service lastUpdate field
func TestService_LastUpdate(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Initially should be zero
	assert.True(t, service.lastUpdate.IsZero())

	// Manually update lastUpdate
	now := time.Now()
	service.mu.Lock()
	service.lastUpdate = now
	service.mu.Unlock()

	// Verify update
	service.mu.RLock()
	updateTime := service.lastUpdate
	service.mu.RUnlock()

	assert.Equal(t, now.Unix(), updateTime.Unix())
}

// Test Service with different config values
func TestService_DifferentConfigs(t *testing.T) {
	testCases := []struct {
		name     string
		config   *config.CCXTConfig
		expected bool
	}{
		{
			name: "valid config",
			config: &config.CCXTConfig{
				ServiceURL: "http://localhost:3001",
				Timeout:    30,
			},
			expected: true,
		},
		{
			name: "different URL",
			config: &config.CCXTConfig{
				ServiceURL: "http://example.com:8080",
				Timeout:    60,
			},
			expected: true,
		},
		{
			name: "zero timeout",
			config: &config.CCXTConfig{
				ServiceURL: "http://localhost:3001",
				Timeout:    0,
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := zaplogrus.New()
			blacklistCache := cache.NewInMemoryBlacklistCache()
			service := NewService(tc.config, logger, blacklistCache)
			assert.NotNil(t, service)
			assert.NotNil(t, service.client)
			assert.Equal(t, tc.expected, service != nil)
		})
	}
}

// Test Service supportedExchanges map operations
func TestService_SupportedExchangesOperations(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Test adding multiple exchanges
	exchanges := map[string]ExchangeInfo{
		"binance":  {ID: "binance", Name: "Binance", Countries: []string{"MT"}},
		"coinbase": {ID: "coinbase", Name: "Coinbase", Countries: []string{"US"}},
		"kraken":   {ID: "kraken", Name: "Kraken", Countries: []string{"US"}},
	}

	service.mu.Lock()
	for id, info := range exchanges {
		service.supportedExchanges[id] = info
	}
	service.mu.Unlock()

	// Test GetSupportedExchanges returns all exchanges
	supportedIDs := service.GetSupportedExchanges()
	assert.Len(t, supportedIDs, 3)
	assert.Contains(t, supportedIDs, "binance")
	assert.Contains(t, supportedIDs, "coinbase")
	assert.Contains(t, supportedIDs, "kraken")

	// Test GetExchangeInfo for each exchange
	for id, expectedInfo := range exchanges {
		info, exists := service.GetExchangeInfo(id)
		assert.True(t, exists, "Exchange %s should exist", id)
		assert.Equal(t, expectedInfo.ID, info.ID)
		assert.Equal(t, expectedInfo.Name, info.Name)
		assert.Equal(t, expectedInfo.Countries, info.Countries)
	}
}

// Test Service edge cases
func TestService_EdgeCases(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Test GetExchangeInfo with empty string
	info, exists := service.GetExchangeInfo("")
	assert.False(t, exists)
	assert.Empty(t, info.ID)

	// Test GetSupportedExchanges multiple times
	exchanges1 := service.GetSupportedExchanges()
	exchanges2 := service.GetSupportedExchanges()
	assert.Equal(t, exchanges1, exchanges2)

	// Test that GetSupportedExchanges is consistent
	assert.Equal(t, len(exchanges1), len(exchanges2))
}

// Test Service initialization state
func TestService_InitializationState(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	// Test initial state
	assert.NotNil(t, service.client)
	assert.NotNil(t, service.supportedExchanges)
	assert.Empty(t, service.supportedExchanges)
	assert.True(t, service.lastUpdate.IsZero())

	// Test that supportedExchanges is properly initialized as a map
	assert.IsType(t, make(map[string]ExchangeInfo), service.supportedExchanges)

	// Test that we can write to the map without panic
	assert.NotPanics(t, func() {
		service.mu.Lock()
		service.supportedExchanges["test"] = ExchangeInfo{ID: "test"}
		service.mu.Unlock()
	})
}

// Test GetServiceURL method
func TestService_GetServiceURL(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	url := service.GetServiceURL()
	assert.Equal(t, cfg.ServiceURL, url)

	// Test with nil client
	service.client = nil
	url = service.GetServiceURL()
	assert.Empty(t, url)
}

// Test Close method
func TestService_Close(t *testing.T) {
	cfg := &config.CCXTConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30,
	}

	logger := zaplogrus.New()
	blacklistCache := cache.NewInMemoryBlacklistCache()
	service := NewService(cfg, logger, blacklistCache)

	err := service.Close()
	assert.NoError(t, err)
}
