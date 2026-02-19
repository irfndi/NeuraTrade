package services

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/logging"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/irfndi/neuratrade/test/testmocks"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMain(m *testing.M) {
	// Some environments (e.g., restricted sandboxes) disallow binding local ports, which breaks miniredis-based tests.
	if ln, err := net.Listen("tcp", "127.0.0.1:0"); err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			fmt.Println("Skipping services package tests: binding not permitted in this environment")
			os.Exit(0)
		}
	} else if ln != nil {
		_ = ln.Close()
	}

	// Initialize a basic logger to prevent nil pointer dereference in tests
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)

	// Initialize telemetry with disabled export to prevent nil pointer dereference
	// This is needed because ExchangeCapabilityCache tries to log via telemetry.Logger()
	telemetryConfig := telemetry.TelemetryConfig{
		Enabled: false, // Disable actual telemetry export for tests
	}

	// This will initialize the global logger in telemetry package
	err := telemetry.InitTelemetry(telemetryConfig)
	if err != nil {
		// Continue even if telemetry fails - we'll use fallback logging
		fmt.Printf("Warning: Failed to initialize telemetry for tests: %v\n", err)
	}

	// Run the tests
	code := m.Run()

	// Clean up
	if err := telemetry.Shutdown(); err != nil {
		fmt.Printf("Failed to shutdown telemetry: %v\n", err)
	}

	// Exit with the same code as the tests
	os.Exit(code)
}

func TestNewCollectorService(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)
	defer collector.Stop()

	assert.NotNil(t, collector)
	assert.NotNil(t, collector.workers)
	assert.Equal(t, 300, collector.collectorConfig.IntervalSeconds)
	assert.Equal(t, 5, collector.collectorConfig.MaxErrors)
}

func TestCollectorService_Start(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}

	// Mock the Initialize and GetSupportedExchanges calls
	mockCCXT.On("Initialize", mock.Anything).Return(nil)
	mockCCXT.On("GetSupportedExchanges").Return([]string{}) // Called once in initializeWorkersAsync

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test that the service can start without errors
	err := collector.Start()
	assert.NoError(t, err)

	// Wait a bit for async initialization to start
	time.Sleep(100 * time.Millisecond)

	// Clean up: stop the collector to prevent it from running indefinitely
	collector.Stop()

	mockCCXT.AssertExpectations(t)
}

func TestCollectorService_Stop(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test that the service can be stopped without errors
	collector.Stop()

	// Verify that the context is cancelled
	select {
	case <-collector.ctx.Done():
		// Context was cancelled as expected
	default:
		t.Error("Expected context to be cancelled")
	}
}

func TestCollectorService_GetWorkerStatus(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Get worker status (should be empty initially)
	status := collector.GetWorkerStatus()
	assert.NotNil(t, status)
	assert.Len(t, status, 0) // No workers created yet
}

func TestCollectorService_IsHealthy(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test with no workers (should be unhealthy)
	assert.False(t, collector.IsHealthy())

	// Add a running worker
	collector.workers["binance"] = &Worker{
		Exchange:  "binance",
		IsRunning: true,
	}

	// Test with one running worker (should be healthy)
	assert.True(t, collector.IsHealthy())

	// Add a stopped worker
	collector.workers["coinbase"] = &Worker{
		Exchange:  "coinbase",
		IsRunning: false,
	}

	// Test with 50% workers running (should be healthy)
	assert.True(t, collector.IsHealthy())

	// Stop the first worker
	collector.workers["binance"].IsRunning = false

	// Test with 0% workers running (should be unhealthy)
	assert.False(t, collector.IsHealthy())
}

func TestCollectorService_RestartWorker(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)
	defer collector.Stop()

	// Test restarting non-existent worker
	err := collector.RestartWorker("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "worker for exchange nonexistent not found")

	// Add a worker with errors
	collector.workers["binance"] = &Worker{
		Exchange:   "binance",
		IsRunning:  false,
		ErrorCount: 5,
		Interval:   60 * time.Second,
		MaxErrors:  5,
	}

	// Test restarting existing worker
	err = collector.RestartWorker("binance")
	assert.NoError(t, err)
	assert.Equal(t, 0, collector.workers["binance"].ErrorCount)
	assert.True(t, collector.workers["binance"].IsRunning)
}

func TestWorker_Struct(t *testing.T) {
	now := time.Now()
	worker := &Worker{
		Exchange:   "binance",
		Symbols:    []string{"BTC/USDT", "ETH/USDT"},
		Interval:   60 * time.Second,
		LastUpdate: now,
		IsRunning:  true,
		ErrorCount: 0,
		MaxErrors:  5,
	}

	assert.Equal(t, "binance", worker.Exchange)
	assert.Len(t, worker.Symbols, 2)
	assert.Equal(t, 60*time.Second, worker.Interval)
	assert.Equal(t, now, worker.LastUpdate)
	assert.True(t, worker.IsRunning)
	assert.Equal(t, 0, worker.ErrorCount)
	assert.Equal(t, 5, worker.MaxErrors)
}

func TestCollectorConfig_Struct(t *testing.T) {
	config := CollectorConfig{
		IntervalSeconds: 30,
		MaxErrors:       10,
	}

	assert.Equal(t, 30, config.IntervalSeconds)
	assert.Equal(t, 10, config.MaxErrors)
}

// Test validateMarketData function
func TestCollectorService_ValidateMarketData(t *testing.T) {
	collector := &CollectorService{}

	tests := []struct {
		name     string
		ticker   *models.MarketPrice
		exchange string
		symbol   string
		wantErr  bool
	}{
		{
			name: "valid data",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			wantErr:  false,
		},
		{
			name: "zero price",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			wantErr:  true,
		},
		{
			name: "negative price",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(-100.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			wantErr:  true,
		},
		{
			name: "extremely high price",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(20000000.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			wantErr:  true,
		},
		{
			name: "negative volume",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.NewFromFloat(-100.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			wantErr:  true,
		},
		{
			name: "future timestamp",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now().Add(2 * time.Hour),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			wantErr:  true,
		},
		{
			name: "old timestamp",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now().Add(-25 * time.Hour),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := collector.validateMarketData(tt.ticker, tt.exchange, tt.symbol)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMarketData() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test parseSymbol function
func TestCollectorService_ParseSymbol(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	tests := []struct {
		name          string
		symbol        string
		expectedBase  string
		expectedQuote string
	}{
		{
			name:          "standard slash format",
			symbol:        "BTC/USDT",
			expectedBase:  "BTC",
			expectedQuote: "USDT",
		},
		{
			name:          "futures format with settlement",
			symbol:        "BTC/USDT:USDT",
			expectedBase:  "BTC",
			expectedQuote: "USDT",
		},
		{
			name:          "concatenated format USDT",
			symbol:        "BTCUSDT",
			expectedBase:  "BTC",
			expectedQuote: "USDT",
		},
		{
			name:          "concatenated format USD",
			symbol:        "BTCUSD",
			expectedBase:  "BTC",
			expectedQuote: "USD",
		},
		{
			name:          "invalid symbol",
			symbol:        "INVALID",
			expectedBase:  "",
			expectedQuote: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, quote := collector.parseSymbol(tt.symbol)
			assert.Equal(t, tt.expectedBase, base)
			assert.Equal(t, tt.expectedQuote, quote)
		})
	}
}

// Test isOptionsContract function
func TestCollectorService_IsOptionsContract(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	tests := []struct {
		name     string
		symbol   string
		expected bool
	}{
		{
			name:     "call option",
			symbol:   "SOLUSDT:USDT-250815-180-C",
			expected: true,
		},
		{
			name:     "put option",
			symbol:   "BTC-25DEC20-20000-P",
			expected: true,
		},
		{
			name:     "regular spot pair",
			symbol:   "BTC/USDT",
			expected: false,
		},
		{
			name:     "futures pair",
			symbol:   "BTC/USDT:USDT",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isOptionsContract(tt.symbol)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test isInvalidSymbolFormat function
func TestCollectorService_IsInvalidSymbolFormat(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	tests := []struct {
		name     string
		symbol   string
		expected bool
	}{
		{
			name:     "valid short symbol",
			symbol:   "BTC/USDT",
			expected: false,
		},
		{
			name:     "too long symbol",
			symbol:   "VERYLONGSYMBOLNAMETHATEXCEEDSLIMITANDISMORETHANFIFTYCHARACTERS",
			expected: true,
		},
		{
			name:     "multiple colons",
			symbol:   "BTC:USDT:USD:EUR",
			expected: true,
		},
		{
			name:     "complex derivative",
			symbol:   "BTC_USDT-PERP_SWAP",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isInvalidSymbolFormat(tt.symbol)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test cacheBulkTickerData function
func TestCollectorService_CacheBulkTickerData(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Create a test collector with nil Redis client (should not panic)
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test data
	marketData := []models.MarketPrice{
		{
			ExchangeName: "binance",
			Symbol:       "BTC/USDT",
			Price:        decimal.NewFromFloat(50000.0),
			Volume:       decimal.NewFromFloat(1000.0),
			Timestamp:    time.Now(),
		},
		{
			ExchangeName: "binance",
			Symbol:       "ETH/USDT",
			Price:        decimal.NewFromFloat(3000.0),
			Volume:       decimal.NewFromFloat(5000.0),
			Timestamp:    time.Now(),
		},
	}

	// Test with nil Redis client - should not panic
	assert.NotPanics(t, func() {
		collector.cacheBulkTickerData("binance", marketData)
	})
}

// Test cacheBulkTickerData with Redis
func TestCollectorService_CacheBulkTickerData_WithRedis(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Create a test Redis instance
	redisServer, err := miniredis.Run()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("miniredis cannot bind in this environment; skipping Redis-backed collector test")
		}
		assert.NoError(t, err)
	}
	defer redisServer.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisServer.Addr(),
	})

	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)
	collector.redisClient = redisClient

	// Test data
	marketData := []models.MarketPrice{
		{
			ExchangeName: "binance",
			Symbol:       "BTC/USDT",
			Price:        decimal.NewFromFloat(50000.0),
			Volume:       decimal.NewFromFloat(1000.0),
			Timestamp:    time.Now(),
		},
		{
			ExchangeName: "binance",
			Symbol:       "ETH/USDT",
			Price:        decimal.NewFromFloat(3000.0),
			Volume:       decimal.NewFromFloat(5000.0),
			Timestamp:    time.Now(),
		},
	}

	// Test with Redis client - should not panic
	assert.NotPanics(t, func() {
		collector.cacheBulkTickerData("binance", marketData)
	})

	// Verify data was cached
	bulkKey := "bulk_tickers:binance"
	cachedData, err := redisClient.Get(context.Background(), bulkKey).Result()
	assert.NoError(t, err)
	assert.NotEmpty(t, cachedData)

	// Verify individual tickers were cached
	individualKey := "ticker:binance:BTC/USDT"
	individualData, err := redisClient.Get(context.Background(), individualKey).Result()
	assert.NoError(t, err)
	assert.NotEmpty(t, individualData)
}

// Test saveBulkTickerData function
func TestCollectorService_SaveBulkTickerData(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test with nil database - should handle error gracefully
	ticker := models.MarketPrice{
		ExchangeName: "binance",
		Symbol:       "BTC/USDT",
		Price:        decimal.NewFromFloat(50000.0),
		Volume:       decimal.NewFromFloat(1000.0),
		Timestamp:    time.Now(),
	}

	// This will panic due to nil database, so we need to recover from it
	defer func() {
		if r := recover(); r != nil {
			// Expected panic when database is nil
			assert.NotNil(t, r)
		}
	}()

	err := collector.saveBulkTickerData(ticker)
	// If we reach here, check for error
	assert.Error(t, err)
}

// Test saveBulkTickerData with nil database (should handle gracefully)
func TestCollectorService_SaveBulkTickerData_NilDatabase(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}

	// Create config with blacklist TTL
	config := &config.Config{
		Blacklist: config.BlacklistConfig{
			TTL: "1h",
		},
	}

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test with valid ticker but nil database - should handle database errors gracefully
	ticker := models.MarketPrice{
		ExchangeName: "binance",
		Symbol:       "BTC/USDT",
		Price:        decimal.NewFromFloat(50000.0),
		Volume:       decimal.NewFromFloat(1000.0),
		Timestamp:    time.Now(),
	}

	// This will panic due to nil database, so we need to recover from it
	defer func() {
		if r := recover(); r != nil {
			// Expected panic when database is nil
			assert.NotNil(t, r)
		}
	}()

	err := collector.saveBulkTickerData(ticker)
	// If we reach here without panicking, check for error
	assert.NotNil(t, err, "Expected error when database is nil")
}

// Test saveBulkTickerData with invalid data (should be blacklisted)
func TestCollectorService_SaveBulkTickerData_InvalidData(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}

	// Create config with blacklist TTL
	config := &config.Config{
		Blacklist: config.BlacklistConfig{
			TTL: "1h",
		},
	}

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test with invalid price (should be blacklisted)
	ticker := models.MarketPrice{
		ExchangeName: "binance",
		Symbol:       "BTC/USDT",
		Price:        decimal.NewFromFloat(0), // Zero price should trigger blacklist
		Volume:       decimal.NewFromFloat(1000.0),
		Timestamp:    time.Now(),
	}

	err := collector.saveBulkTickerData(ticker)
	assert.NoError(t, err) // Should return nil for blacklisted tickers

	// Check if symbol was added to blacklist
	symbolKey := "binance:BTC/USDT"
	blacklisted, _ := blacklistCache.IsBlacklisted(symbolKey)
	assert.True(t, blacklisted, "Symbol should be blacklisted")
}

// Test collectTickerDataDirect function
func TestCollectorService_CollectTickerDataDirect(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test with blacklisted symbol
	blacklistCache.Add("binance:BTC/USDT", "invalid_data", time.Hour)
	err := collector.collectTickerDataDirect("binance", "BTC/USDT")
	assert.NoError(t, err) // Should return nil for blacklisted symbols

	// Test with valid symbol (should fail gracefully due to nil dependencies)
	// This will panic due to nil database, so we need to recover from it
	defer func() {
		if r := recover(); r != nil {
			// Expected panic when database is nil
			assert.NotNil(t, r)
		}
	}()

	err = collector.collectTickerDataDirect("binance", "ETH/USDT")
	// If we reach here without panicking, check for error
	assert.Error(t, err) // Should error due to nil database and other dependencies
	assert.True(t, assert.Contains(t, err.Error(), "failed to fetch ticker data") ||
		assert.Contains(t, err.Error(), "database pool is not available"))
}

// Test collectTickerDataDirect with mock CCXT service
func TestCollectorService_CollectTickerDataDirect_WithMockCCXT(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Setup mock response for successful ticker fetch
	validTicker := &models.MarketPrice{
		ExchangeName: "binance",
		Symbol:       "ETH/USDT",
		Price:        decimal.NewFromFloat(3000.0),
		Volume:       decimal.NewFromFloat(5000.0),
		Timestamp:    time.Now(),
	}

	// Mock the CCXT service to return successful data
	mockCCXT.On("FetchSingleTicker", mock.Anything, "binance", "ETH/USDT").Return(validTicker, nil)

	// Test with valid symbol (should still fail due to nil database but get past CCXT call)
	// This will panic due to nil database, so we need to recover from it
	defer func() {
		if r := recover(); r != nil {
			// Expected panic when database is nil
			assert.NotNil(t, r)
		}
	}()

	err := collector.collectTickerDataDirect("binance", "ETH/USDT")
	// If we reach here without panicking, check for error
	assert.Error(t, err)
	// The error should be about database or exchange creation, not CCXT fetch
	assert.True(t, assert.Contains(t, err.Error(), "failed to get or create exchange") ||
		assert.Contains(t, err.Error(), "database pool is not available"))

	mockCCXT.AssertExpectations(t)
}

// Test collectTickerDataDirect with CCXT error (should blacklist)
func TestCollectorService_CollectTickerDataDirect_CCXErrorWithBlacklist(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}

	// Create config with blacklist TTL
	config := &config.Config{
		Blacklist: config.BlacklistConfig{
			TTL: "1h",
		},
	}

	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Mock the CCXT service to return an error that should trigger blacklisting
	ccxtError := fmt.Errorf("invalid symbol: symbol not found")
	mockCCXT.On("FetchSingleTicker", mock.Anything, "binance", "INVALID/USDT").Return((*models.MarketPrice)(nil), ccxtError)

	// Test with invalid symbol (should be blacklisted)
	err := collector.collectTickerDataDirect("binance", "INVALID/USDT")
	assert.NoError(t, err) // Should return nil for blacklisted symbols

	// Check if symbol was added to blacklist
	symbolKey := "binance:INVALID/USDT"
	blacklisted, _ := blacklistCache.IsBlacklisted(symbolKey)
	assert.True(t, blacklisted, "Symbol should be blacklisted after CCXT error")

	mockCCXT.AssertExpectations(t)
}

// Test collectFundingRates function (wrapper for collectFundingRatesBulk)
func TestCollectorService_CollectFundingRates(t *testing.T) {
	// Initialize a basic logger for testing to prevent nil pointer dereference
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel) // Reduce log noise in tests

	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Create a worker for testing
	worker := &Worker{
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT", "ETH/USDT"},
	}

	// Mock the CCXT service for funding rates
	mockCCXT.On("FetchAllFundingRates", mock.Anything, "binance").Return([]ccxt.FundingRate{}, fmt.Errorf("funding rates not supported"))

	// Test the wrapper function (should call collectFundingRatesBulk)
	// We expect this to return nil when exchange doesn't support funding rates (handled gracefully)
	assert.NotPanics(t, func() {
		err := collector.collectFundingRates(worker)
		// Should return nil when funding rates are not supported (this is the expected behavior)
		assert.NoError(t, err)
	})

	mockCCXT.AssertExpectations(t)
}

// Test collectFundingRatesBulk function with comprehensive scenarios
func TestCollectorService_CollectFundingRatesBulk(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Create a worker for testing
	worker := &Worker{
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT", "ETH/USDT"},
	}

	// Mock FetchAllFundingRates to handle the actual call that will be made
	mockCCXT.On("FetchAllFundingRates", mock.Anything, "binance").Return([]ccxt.FundingRate{}, fmt.Errorf("funding rates not supported"))

	// Test with mock CCXT that doesn't support funding rates
	// The function should return nil when exchange doesn't support funding rates (handled gracefully)
	err := collector.collectFundingRatesBulk(worker)
	assert.NoError(t, err)
	// Should handle case where exchange doesn't support funding rates gracefully

	// Test with successful funding rates but nil database (should panic due to nil database access)
	mockCCXT.ExpectedCalls = nil // Clear previous expectations

	// Mock CCXT to return error to avoid database access issues in goroutines
	mockCCXT.On("FetchAllFundingRates", mock.Anything, "binance").Return([]ccxt.FundingRate{}, fmt.Errorf("funding rates not available"))

	collector.ccxtService = mockCCXT

	// Test concurrent processing - should handle unsupported funding rates gracefully without panicking
	err = collector.collectFundingRatesBulk(worker)
	// Should return nil when exchange doesn't support funding rates (not an error)
	assert.NoError(t, err)
	mockCCXT.AssertExpectations(t)
}

// Test storeFundingRate function
func TestCollectorService_StoreFundingRate(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered from panic in TestCollectorService_StoreFundingRate: %v\n", r)
		}
	}()

	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	// Test funding rate data
	fundingRate := ccxt.FundingRate{
		Symbol:           "BTC/USDT",
		FundingRate:      0.0001,
		FundingTimestamp: ccxt.UnixTimestamp(time.Now()),
		MarkPrice:        50000.0,
		IndexPrice:       50000.0,
	}

	// Test with nil database - should panic due to nil database access
	assert.Panics(t, func() {
		err := collector.storeFundingRate("binance", fundingRate)
		if err != nil {
			t.Logf("Error: %v", err)
		}
	})

	// Test with Redis client (should handle cache invalidation)
	redisServer, err := miniredis.Run()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("miniredis cannot bind in this environment; skipping Redis-backed collector test")
		}
		assert.NoError(t, err)
	}
	defer redisServer.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisServer.Addr(),
	})
	collector.redisClient = redisClient

	// Set some cache keys that should be invalidated
	redisClient.Set(context.Background(), "bulk_tickers:binance", "test_data", 0)
	redisClient.Set(context.Background(), "ticker:binance:BTC/USDT", "test_data", 0)
	redisClient.Set(context.Background(), "market_stats:binance", "test_data", 0)

	// Test with Redis but nil database (should still panic due to nil database access)
	assert.Panics(t, func() {
		err := collector.storeFundingRate("binance", fundingRate)
		if err != nil {
			t.Logf("Error: %v", err)
		}
	})

	// Verify cache keys still exist (since function panicked before cache invalidation)
	_, err = redisClient.Get(context.Background(), "bulk_tickers:binance").Result()
	assert.NoError(t, err)
	_, err = redisClient.Get(context.Background(), "ticker:binance:BTC/USDT").Result()
	assert.NoError(t, err)
	_, err = redisClient.Get(context.Background(), "market_stats:binance").Result()
	assert.NoError(t, err)
}

// Test collectFundingRatesBulk with exchange capability caching
func TestCollectorService_CollectFundingRatesBulk_CapabilityCaching(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	worker := &Worker{
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT"},
	}

	// Mock exchange that doesn't support funding rates
	// The implementation calls FetchAllFundingRates directly, not GetExchangeInfo
	// Note: The error recovery manager will retry this call multiple times
	mockCCXT.On("FetchAllFundingRates", mock.Anything, "binance").Return([]ccxt.FundingRate{}, fmt.Errorf("funding rates not supported"))

	collector.ccxtService = mockCCXT

	// First call should determine exchange doesn't support funding rates and cache this
	err := collector.collectFundingRatesBulk(worker)
	assert.NoError(t, err) // Should return nil when funding rates are not supported

	// Second call should use cached capability and not call FetchAllFundingRates again
	// (since the capability is now cached as not supporting funding rates)
	err = collector.collectFundingRatesBulk(worker)
	assert.NoError(t, err) // Should return nil when funding rates are not supported

	// Verify that FetchAllFundingRates was called (may be called multiple times due to retries)
	mockCCXT.AssertExpectations(t)
}

// Test collectFundingRatesBulk with CCXT errors (circuit breaker pattern)
func TestCollectorService_CollectFundingRatesBulk_CCXTErrors(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	worker := &Worker{
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT"},
	}

	// Mock CCXT error
	ccxtError := fmt.Errorf("exchange API error")
	mockCCXT.On("FetchAllFundingRates", mock.Anything, "binance").Return([]ccxt.FundingRate{}, ccxtError)

	collector.ccxtService = mockCCXT

	// Test with CCXT error - should handle gracefully
	err := collector.collectFundingRatesBulk(worker)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exchange API error")

	mockCCXT.AssertExpectations(t)
}

// Test collectFundingRatesBulk with concurrent processing
func TestCollectorService_CollectFundingRatesBulk_Concurrent(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	config := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	// Use nil database to test error handling
	collector := NewCollectorService(nil, mockCCXT, config, nil, blacklistCache)

	worker := &Worker{
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}, // Multiple symbols for concurrent testing
	}

	// Mock CCXT to return error to avoid database access issues
	mockCCXT.On("FetchAllFundingRates", mock.Anything, "binance").Return([]ccxt.FundingRate{}, fmt.Errorf("funding rates not available"))

	collector.ccxtService = mockCCXT

	// Test concurrent processing - should handle CCXT error gracefully without panicking
	err := collector.collectFundingRatesBulk(worker)
	// Should return an error when CCXT fails, but not panic
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch funding rates")

	mockCCXT.AssertExpectations(t)
}

// Test SymbolCache Get method (currently 0% coverage)
func TestSymbolCache_Get(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	cache := NewSymbolCache(5*time.Minute, logger)

	// Test getting from empty cache
	symbols, found := cache.Get("binance")
	assert.False(t, found)
	assert.Nil(t, symbols)

	// Set some data first
	cache.Set("binance", []string{"BTC/USDT", "ETH/USDT"})

	// Test getting existing data
	symbols, found = cache.Get("binance")
	assert.True(t, found)
	assert.Equal(t, []string{"BTC/USDT", "ETH/USDT"}, symbols)

	// Test getting non-existent data
	symbols, found = cache.Get("coinbase")
	assert.False(t, found)
	assert.Nil(t, symbols)

	// Test with empty exchange ID
	symbols, found = cache.Get("")
	assert.False(t, found)
	assert.Nil(t, symbols)
}

// Test SymbolCache Set method (currently 0% coverage)
func TestSymbolCache_Set(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	cache := NewSymbolCache(5*time.Minute, logger)

	// Test setting new data
	cache.Set("binance", []string{"BTC/USDT", "ETH/USDT"})

	// Verify data was set
	symbols, found := cache.Get("binance")
	assert.True(t, found)
	assert.Equal(t, []string{"BTC/USDT", "ETH/USDT"}, symbols)

	// Test updating existing data
	cache.Set("binance", []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"})

	// Verify data was updated
	symbols, found = cache.Get("binance")
	assert.True(t, found)
	assert.Equal(t, []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}, symbols)

	// Test setting empty slice
	cache.Set("coinbase", []string{})

	// Verify empty slice was set
	symbols, found = cache.Get("coinbase")
	assert.True(t, found)
	assert.Equal(t, []string{}, symbols)

	// Test setting nil slice
	cache.Set("kucoin", nil)

	// Verify nil slice was set
	symbols, found = cache.Get("kucoin")
	assert.True(t, found)
	assert.Nil(t, symbols)

	// Test with empty exchange ID
	cache.Set("", []string{"TEST/USDT"})

	// Verify empty exchange ID was handled
	symbols, found = cache.Get("")
	assert.True(t, found)
	assert.Equal(t, []string{"TEST/USDT"}, symbols)
}

// Test SymbolCache Stats methods (currently 0% coverage)
func TestSymbolCache_GetStats(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	cache := NewSymbolCache(5*time.Minute, logger)

	// Test initial stats
	stats := cache.GetStats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
	assert.Equal(t, int64(0), stats.Sets)

	// Perform some operations
	cache.Set("binance", []string{"BTC/USDT"})
	cache.Get("binance")  // Hit
	cache.Get("coinbase") // Miss
	cache.Get("coinbase") // Miss

	// Test updated stats
	stats = cache.GetStats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(2), stats.Misses)
	assert.Equal(t, int64(1), stats.Sets)
}

// Test SymbolCache concurrent operations
func TestSymbolCache_ConcurrentOperations(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	cache := NewSymbolCache(5*time.Minute, logger)

	// Test concurrent Set operations
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			exchangeID := fmt.Sprintf("exchange%d", id)
			cache.Set(exchangeID, []string{fmt.Sprintf("SYMBOL%d/USDT", id)})
			done <- true
		}(i)
	}

	// Wait for all Set operations to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all data was set correctly
	for i := 0; i < 10; i++ {
		exchangeID := fmt.Sprintf("exchange%d", i)
		expectedSymbol := fmt.Sprintf("SYMBOL%d/USDT", i)

		symbols, found := cache.Get(exchangeID)
		assert.True(t, found)
		assert.Equal(t, []string{expectedSymbol}, symbols)
		// Verify cache state through GetStats instead of IsInitialized
		stats := cache.GetStats()
		assert.Equal(t, int64(10), stats.Sets) // We Set 10 entries
	}

	// Test concurrent Get operations
	done = make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func(id int) {
			exchangeID := fmt.Sprintf("exchange%d", id%10) // Access existing exchanges
			cache.Get(exchangeID)
			cache.GetStats() // Use GetStats instead of IsInitialized
			done <- true
		}(i)
	}

	// Wait for all Get operations to complete
	for i := 0; i < 20; i++ {
		<-done
	}
}

// Test SymbolCache edge cases
func TestSymbolCache_EdgeCases(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	cache := NewSymbolCache(5*time.Minute, logger)

	// Test multiple updates to same key
	cache.Set("binance", []string{"BTC/USDT"})
	cache.Set("binance", []string{"ETH/USDT"})
	cache.Set("binance", []string{"SOL/USDT"})

	// Verify final value
	symbols, found := cache.Get("binance")
	assert.True(t, found)
	assert.Equal(t, []string{"SOL/USDT"}, symbols)

	// Test setting and getting very long symbol lists
	longList := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		longList[i] = fmt.Sprintf("SYMBOL%d/USDT", i)
	}

	cache.Set("large_exchange", longList)
	symbols, found = cache.Get("large_exchange")
	assert.True(t, found)
	assert.Equal(t, longList, symbols)
	assert.Equal(t, 1000, len(symbols))

	// Test setting and getting special characters in exchange IDs
	cache.Set("exchange-with.dash", []string{"BTC/USDT"})
	cache.Set("exchange_with_underscore", []string{"ETH/USDT"})
	cache.Set("exchange.with.dots", []string{"SOL/USDT"})

	symbols, found = cache.Get("exchange-with.dash")
	assert.True(t, found)
	assert.Equal(t, []string{"BTC/USDT"}, symbols)

	symbols, found = cache.Get("exchange_with_underscore")
	assert.True(t, found)
	assert.Equal(t, []string{"ETH/USDT"}, symbols)

	symbols, found = cache.Get("exchange.with.dots")
	assert.True(t, found)
	assert.Equal(t, []string{"SOL/USDT"}, symbols)
}

// Test SymbolCache LogStats method (currently 0% coverage)
func TestSymbolCache_LogStats(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	cache := NewSymbolCache(5*time.Minute, logger)

	// Test LogStats doesn't panic and logs properly
	// Since LogStats just logs to the logger, we just ensure it doesn't panic
	assert.NotPanics(t, func() {
		cache.LogStats()
	})

	// Perform some operations to generate stats
	cache.Set("binance", []string{"BTC/USDT"})
	cache.Get("binance")  // Hit
	cache.Get("coinbase") // Miss

	// Test LogStats with actual data
	assert.NotPanics(t, func() {
		cache.LogStats()
	})
}

// TestCollectorService_ConvertMarketPriceInterfacesToModels tests the convertMarketPriceInterfacesToModels function
func TestCollectorService_ConvertMarketPriceInterfacesToModels(t *testing.T) {
	// Create a collector service instance
	service := &CollectorService{}

	// Test with empty slice
	result := service.convertMarketPriceInterfacesToModels([]ccxt.MarketPriceInterface{})
	assert.NotNil(t, result)
	assert.Empty(t, result)

	// Test with nil slice
	result = service.convertMarketPriceInterfacesToModels(nil)
	assert.NotNil(t, result)
	assert.Empty(t, result)

	// Test with single item
	singleItem := &MockMarketPriceInterface{
		exchangeName: "binance",
		symbol:       "BTC/USDT",
		price:        50000.0,
		volume:       1000.0,
		timestamp:    time.Now(),
	}

	result = service.convertMarketPriceInterfacesToModels([]ccxt.MarketPriceInterface{singleItem})
	assert.NotNil(t, result)
	assert.Len(t, result, 1)

	convertedItem := result[0]
	assert.Equal(t, 0, convertedItem.ExchangeID) // Should be 0 as per implementation
	assert.Equal(t, "binance", convertedItem.ExchangeName)
	assert.Equal(t, "BTC/USDT", convertedItem.Symbol)
	assert.Equal(t, "50000", convertedItem.Price.String())
	assert.Equal(t, "1000", convertedItem.Volume.String())
	assert.Equal(t, singleItem.GetTimestamp(), convertedItem.Timestamp)

	// Test with multiple items
	multipleItems := []ccxt.MarketPriceInterface{
		singleItem,
		&MockMarketPriceInterface{
			exchangeName: "coinbase",
			symbol:       "ETH/USD",
			price:        3000.0,
			volume:       500.0,
			timestamp:    time.Now().Add(-1 * time.Hour),
		},
	}

	result = service.convertMarketPriceInterfacesToModels(multipleItems)
	assert.NotNil(t, result)
	assert.Len(t, result, 2)

	// Verify first item
	assert.Equal(t, "binance", result[0].ExchangeName)
	assert.Equal(t, "BTC/USDT", result[0].Symbol)

	// Verify second item
	assert.Equal(t, "coinbase", result[1].ExchangeName)
	assert.Equal(t, "ETH/USD", result[1].Symbol)
	assert.Equal(t, "3000", result[1].Price.String())
	assert.Equal(t, "500", result[1].Volume.String())

	// Test with decimal precision
	preciseItem := &MockMarketPriceInterface{
		exchangeName: "kraken",
		symbol:       "BTC/USD",
		price:        50000.12345678,
		volume:       1000.98765432,
		timestamp:    time.Now(),
	}

	result = service.convertMarketPriceInterfacesToModels([]ccxt.MarketPriceInterface{preciseItem})
	assert.NotNil(t, result)
	assert.Len(t, result, 1)

	preciseResult := result[0]
	assert.Equal(t, "50000.12345678", preciseResult.Price.String())
	assert.Equal(t, "1000.98765432", preciseResult.Volume.String())

	// Test bid/ask preservation - critical for arbitrage detection
	bidAskItem := &MockMarketPriceInterface{
		exchangeName: "binance",
		symbol:       "BTC/USDT",
		price:        50000.0,
		bid:          49999.50,
		ask:          50000.50,
		volume:       1000.0,
		timestamp:    time.Now(),
	}

	result = service.convertMarketPriceInterfacesToModels([]ccxt.MarketPriceInterface{bidAskItem})
	assert.NotNil(t, result)
	assert.Len(t, result, 1)

	bidAskResult := result[0]
	assert.Equal(t, "49999.5", bidAskResult.Bid.String(), "Bid should be preserved during conversion")
	assert.Equal(t, "50000.5", bidAskResult.Ask.String(), "Ask should be preserved during conversion")
	assert.Equal(t, "50000", bidAskResult.Price.String())
}

// TestCollectorService_ConvertMarketPriceInterfaceToModel tests the convertMarketPriceInterfaceToModel function
func TestCollectorService_ConvertMarketPriceInterfaceToModel(t *testing.T) {
	// Create a collector service instance
	service := &CollectorService{}

	// Test with nil input
	result := service.convertMarketPriceInterfaceToModel(nil)
	assert.Nil(t, result)

	// Test with valid input
	input := &MockMarketPriceInterface{
		exchangeName: "binance",
		symbol:       "BTC/USDT",
		price:        50000.0,
		volume:       1000.0,
		timestamp:    time.Now(),
	}

	result = service.convertMarketPriceInterfaceToModel(input)
	assert.NotNil(t, result)

	// Verify all fields are properly converted
	assert.Equal(t, 0, result.ExchangeID) // Should be 0 as per implementation
	assert.Equal(t, "binance", result.ExchangeName)
	assert.Equal(t, "BTC/USDT", result.Symbol)
	assert.Equal(t, "50000", result.Price.String())
	assert.Equal(t, "1000", result.Volume.String())
	assert.Equal(t, input.GetTimestamp(), result.Timestamp)

	// Test with zero values
	zeroInput := &MockMarketPriceInterface{
		exchangeName: "",
		symbol:       "",
		price:        0.0,
		volume:       0.0,
		timestamp:    time.Time{},
	}

	result = service.convertMarketPriceInterfaceToModel(zeroInput)
	assert.NotNil(t, result)
	assert.Equal(t, "", result.ExchangeName)
	assert.Equal(t, "", result.Symbol)
	assert.Equal(t, "0", result.Price.String())
	assert.Equal(t, "0", result.Volume.String())
	assert.Equal(t, time.Time{}, result.Timestamp)

	// Test with negative values (should still work)
	negativeInput := &MockMarketPriceInterface{
		exchangeName: "test",
		symbol:       "TEST/USD",
		price:        -100.0,
		volume:       -50.0,
		timestamp:    time.Now(),
	}

	result = service.convertMarketPriceInterfaceToModel(negativeInput)
	assert.NotNil(t, result)
	assert.Equal(t, "-100", result.Price.String())
	assert.Equal(t, "-50", result.Volume.String())

	// Test with very large numbers
	largeInput := &MockMarketPriceInterface{
		exchangeName: "test",
		symbol:       "LARGE/USD",
		price:        999999999.999,
		volume:       888888888.888,
		timestamp:    time.Now(),
	}

	result = service.convertMarketPriceInterfaceToModel(largeInput)
	assert.NotNil(t, result)
	assert.Equal(t, "999999999.999", result.Price.String())
	assert.Equal(t, "888888888.888", result.Volume.String())

	// Test bid/ask preservation - critical for arbitrage detection
	bidAskInput := &MockMarketPriceInterface{
		exchangeName: "binance",
		symbol:       "BTC/USDT",
		price:        50000.0,
		bid:          49999.50,
		ask:          50000.50,
		volume:       1000.0,
		timestamp:    time.Now(),
	}

	result = service.convertMarketPriceInterfaceToModel(bidAskInput)
	assert.NotNil(t, result)
	assert.Equal(t, "49999.5", result.Bid.String(), "Bid should be preserved during conversion")
	assert.Equal(t, "50000.5", result.Ask.String(), "Ask should be preserved during conversion")
	assert.Equal(t, "50000", result.Price.String())
}

// TestCollectorService_ConvertMarketPriceInterfaceToModel_FunctionalInterface tests with functional interface implementation
func TestCollectorService_ConvertMarketPriceInterfaceToModel_FunctionalInterface(t *testing.T) {
	// Create a collector service instance
	service := &CollectorService{}

	// Test with functional interface implementation
	input := &MockMarketPriceInterface{
		exchangeName: "functional",
		symbol:       "FUNC/USD",
		price:        123.45,
		volume:       67.89,
		timestamp:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	result := service.convertMarketPriceInterfaceToModel(input)
	assert.NotNil(t, result)

	// Verify all fields are properly converted
	assert.Equal(t, 0, result.ExchangeID)
	assert.Equal(t, "functional", result.ExchangeName)
	assert.Equal(t, "FUNC/USD", result.Symbol)
	assert.Equal(t, "123.45", result.Price.String())
	assert.Equal(t, "67.89", result.Volume.String())
	assert.Equal(t, time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC), result.Timestamp)
}

// MockMarketPriceInterface is a mock implementation of MarketPriceInterface for testing
type MockMarketPriceInterface struct {
	exchangeName string
	symbol       string
	price        float64
	bid          float64
	ask          float64
	volume       float64
	high         float64
	low          float64
	timestamp    time.Time
}

func (m *MockMarketPriceInterface) GetExchangeName() string {
	return m.exchangeName
}

func (m *MockMarketPriceInterface) GetSymbol() string {
	return m.symbol
}

func (m *MockMarketPriceInterface) GetPrice() float64 {
	return m.price
}

func (m *MockMarketPriceInterface) GetBid() float64 {
	return m.bid
}

func (m *MockMarketPriceInterface) GetAsk() float64 {
	return m.ask
}

func (m *MockMarketPriceInterface) GetVolume() float64 {
	return m.volume
}

func (m *MockMarketPriceInterface) GetHigh() float64 {
	return m.high
}

func (m *MockMarketPriceInterface) GetLow() float64 {
	return m.low
}

func (m *MockMarketPriceInterface) GetTimestamp() time.Time {
	return m.timestamp
}

func TestCollectorService_IsInitialized(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	cfg := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	collector := NewCollectorService(nil, mockCCXT, cfg, nil, blacklistCache)

	assert.False(t, collector.IsInitialized())

	collector.readinessMu.Lock()
	collector.isInitialized = true
	collector.readinessMu.Unlock()

	assert.True(t, collector.IsInitialized())
}

func TestCollectorService_IsReady(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	cfg := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	collector := NewCollectorService(nil, mockCCXT, cfg, nil, blacklistCache)

	assert.False(t, collector.IsReady())

	collector.readinessMu.Lock()
	collector.isReady = true
	collector.readinessMu.Unlock()

	assert.True(t, collector.IsReady())
}

func TestCollectorService_GetReadinessStatus(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	cfg := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	collector := NewCollectorService(nil, mockCCXT, cfg, nil, blacklistCache)

	init, ready := collector.GetReadinessStatus()
	assert.False(t, init)
	assert.False(t, ready)

	collector.readinessMu.Lock()
	collector.isInitialized = true
	collector.isReady = true
	collector.readinessMu.Unlock()

	init, ready = collector.GetReadinessStatus()
	assert.True(t, init)
	assert.True(t, ready)

	collector.readinessMu.Lock()
	collector.isInitialized = true
	collector.isReady = false
	collector.readinessMu.Unlock()

	init, ready = collector.GetReadinessStatus()
	assert.True(t, init)
	assert.False(t, ready)
}

func TestCollectorService_ReadinessConcurrent(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	cfg := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	collector := NewCollectorService(nil, mockCCXT, cfg, nil, blacklistCache)

	done := make(chan bool, 100)

	for i := 0; i < 50; i++ {
		go func() {
			_ = collector.IsInitialized()
			done <- true
		}()
		go func() {
			_ = collector.IsReady()
			done <- true
		}()
		go func() {
			_, _ = collector.GetReadinessStatus()
			done <- true
		}()
	}

	for i := 0; i < 150; i++ {
		<-done
	}

	collector.readinessMu.Lock()
	collector.isInitialized = true
	collector.isReady = true
	collector.readinessMu.Unlock()

	for i := 0; i < 50; i++ {
		go func() {
			_ = collector.IsInitialized()
			done <- true
		}()
		go func() {
			_ = collector.IsReady()
			done <- true
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestCollectorService_ReadinessInitialState(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	cfg := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	collector := NewCollectorService(nil, mockCCXT, cfg, nil, blacklistCache)

	assert.False(t, collector.IsInitialized())
	assert.False(t, collector.IsReady())

	init, ready := collector.GetReadinessStatus()
	assert.False(t, init)
	assert.False(t, ready)
}

func TestCollectorService_CheckPriceOutlier(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	cfg := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	collector := NewCollectorService(nil, mockCCXT, cfg, nil, blacklistCache)

	tests := []struct {
		name        string
		ticker      *models.MarketPrice
		exchange    string
		symbol      string
		setupCache  func()
		wantErr     bool
		errContains string
	}{
		{
			name: "first price - no comparison available",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			setupCache: func() {
				collector.lastPrice.Delete("binance:BTC/USDT")
			},
			wantErr: false,
		},
		{
			name: "normal price movement",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50100.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now().Add(45 * time.Second),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			setupCache: func() {
				collector.lastPrice.Store("binance:BTC/USDT", priceCacheEntry{
					price:     decimal.NewFromFloat(50000.0),
					timestamp: time.Now(),
				})
			},
			wantErr: false,
		},
		{
			name: "price outlier - 60% move",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(80000.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now().Add(45 * time.Second),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			setupCache: func() {
				collector.lastPrice.Store("binance:BTC/USDT", priceCacheEntry{
					price:     decimal.NewFromFloat(50000.0),
					timestamp: time.Now(),
				})
			},
			wantErr:     true,
			errContains: "price outlier detected",
		},
		{
			name: "price outlier - 80% drop",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(10000.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now().Add(45 * time.Second),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			setupCache: func() {
				collector.lastPrice.Store("binance:BTC/USDT", priceCacheEntry{
					price:     decimal.NewFromFloat(50000.0),
					timestamp: time.Now(),
				})
			},
			wantErr:     true,
			errContains: "price outlier detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.lastPrice = sync.Map{}
			if tt.setupCache != nil {
				tt.setupCache()
			}

			err := collector.checkPriceOutlier(tt.ticker, tt.exchange, tt.symbol)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCollectorService_CheckVolumeAnomaly(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	cfg := &config.Config{}
	blacklistCache := cache.NewInMemoryBlacklistCache()

	collector := NewCollectorService(nil, mockCCXT, cfg, nil, blacklistCache)

	tests := []struct {
		name       string
		ticker     *models.MarketPrice
		exchange   string
		symbol     string
		setupCache func()
		wantErr    bool
	}{
		{
			name: "zero volume - skip check",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.Zero,
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			setupCache: func() {
				collector.volumeStats.Delete("binance:BTC/USDT")
			},
			wantErr: false,
		},
		{
			name: "first 5 samples - accumulate",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.NewFromFloat(1000.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			setupCache: func() {
				collector.volumeStats.Delete("binance:BTC/USDT")
			},
			wantErr: false,
		},
		{
			name: "normal volume after warmup",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.NewFromFloat(1050.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			setupCache: func() {
				collector.volumeStats.Store("binance:BTC/USDT", volumeStatsEntry{
					avgVolume:   decimal.NewFromFloat(1000.0),
					sampleCount: 10,
				})
			},
			wantErr: false,
		},
		{
			name: "high volume anomaly - 15x average",
			ticker: &models.MarketPrice{
				Price:     decimal.NewFromFloat(50000.0),
				Volume:    decimal.NewFromFloat(15000.0),
				Timestamp: time.Now(),
			},
			exchange: "binance",
			symbol:   "BTC/USDT",
			setupCache: func() {
				collector.volumeStats.Store("binance:BTC/USDT", volumeStatsEntry{
					avgVolume:   decimal.NewFromFloat(1000.0),
					sampleCount: 10,
				})
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.volumeStats = sync.Map{}
			if tt.setupCache != nil {
				tt.setupCache()
			}

			err := collector.checkVolumeAnomaly(tt.ticker, tt.exchange, tt.symbol)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
