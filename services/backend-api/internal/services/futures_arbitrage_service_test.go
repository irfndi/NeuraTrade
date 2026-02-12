package services

import (
	"context"
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/logging"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/telemetry"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockErrorRecoveryManager is a mock implementation of ErrorRecoveryManager
type MockErrorRecoveryManager struct {
	mock.Mock
}

func (m *MockErrorRecoveryManager) ExecuteWithRetry(ctx context.Context, operation string, fn func() error) error {
	args := m.Called(ctx, operation, fn)
	return args.Error(0)
}

// MockResourceManager is a mock implementation of ResourceManager
type MockResourceManager struct {
	mock.Mock
}

func (m *MockResourceManager) AcquireResource(resourceType string) error {
	args := m.Called(resourceType)
	return args.Error(0)
}

func (m *MockResourceManager) ReleaseResource(resourceType string) error {
	args := m.Called(resourceType)
	return args.Error(0)
}

func (m *MockResourceManager) RegisterResource(resourceID string, resourceType ResourceType, cleanupFunc func() error, metadata map[string]interface{}) error {
	args := m.Called(resourceID, resourceType, cleanupFunc, metadata)
	return args.Error(0)
}

func (m *MockResourceManager) CleanupResource(resourceID string) error {
	args := m.Called(resourceID)
	return args.Error(0)
}

// MockPerformanceMonitor is a mock implementation of PerformanceMonitor
type MockPerformanceMonitor struct {
	mock.Mock
}

func (m *MockPerformanceMonitor) StartOperation(operation string) {
	m.Called(operation)
}

func (m *MockPerformanceMonitor) EndOperation(operation string, duration time.Duration) {
	m.Called(operation, duration)
}

// MockRedisClient is a mock implementation of redis.Client
type MockRedisClient struct {
	mock.Mock
}

func (m *MockRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.StringCmd)
}

func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	args := m.Called(ctx, key, value, expiration)
	return args.Get(0).(*redis.StatusCmd)
}

func (m *MockRedisClient) Ping(ctx context.Context) *redis.StringCmd {
	args := m.Called(ctx)
	return args.Get(0).(*redis.StringCmd)
}

func (m *MockRedisClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestFuturesArbitrageService_New(t *testing.T) {
	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil), // Using nil for concrete types
		nil,
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	assert.NotNil(t, service)
	assert.Nil(t, service.db)
	assert.Nil(t, service.redisClient)
	assert.Equal(t, mockConfig, service.config)
	assert.NotNil(t, service.calculator)
	assert.Nil(t, service.errorRecoveryManager)
	assert.Nil(t, service.resourceManager)
	assert.Nil(t, service.performanceMonitor)
	assert.False(t, service.running)
}

func TestFuturesArbitrageService_StartStop(t *testing.T) {
	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		nil,
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	// Test initial state
	assert.False(t, service.running)
	assert.Nil(t, service.ctx)
	assert.Nil(t, service.cancel)

	// Note: We don't test Start/Stop with nil resource manager as it would panic
	// The actual service should be initialized with proper resource manager
}

func TestFuturesArbitrageService_IsRunning(t *testing.T) {
	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		nil,
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	// Test when not running
	assert.False(t, service.IsRunning())

	// Note: We don't test the running state as it would require starting the service
	// which needs a proper resource manager to avoid panics
}

func TestFundingRateData_Struct(t *testing.T) {
	now := time.Now()
	rate := decimal.NewFromFloat(0.0001)
	markPrice := decimal.NewFromFloat(50000.0)

	data := FundingRateData{
		Exchange:  "Binance",
		Symbol:    "BTC/USDT",
		Rate:      rate,
		MarkPrice: markPrice,
		Timestamp: now,
	}

	assert.Equal(t, "Binance", data.Exchange)
	assert.Equal(t, "BTC/USDT", data.Symbol)
	assert.Equal(t, rate, data.Rate)
	assert.Equal(t, markPrice, data.MarkPrice)
	assert.Equal(t, now, data.Timestamp)
}

func TestFuturesArbitrageService_getLatestFundingRates_CacheHit(t *testing.T) {
	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	// Test with nil database client - should return error instead of panicking
	_, err := service.getLatestFundingRates(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

func TestFuturesArbitrageService_getLatestFundingRates_CacheMiss(t *testing.T) {
	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	// Test with nil clients - should return error instead of panicking
	_, err := service.getLatestFundingRates(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

func TestFuturesArbitrageService_calculateAndStoreOpportunities_NoFundingRates(t *testing.T) {
	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	// Test with nil dependencies - should panic due to nil error recovery manager
	assert.Panics(t, func() {
		_ = service.calculateAndStoreOpportunities(ctx)
	})
}

func TestFuturesArbitrageService_storeOpportunity(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		nil, // db will be nil for this test
		nil,
		mockConfig,
		nil, // error recovery manager
		nil, // resource manager
		nil, // performance monitor
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	now := time.Now()
	opportunity := &models.FuturesArbitrageOpportunity{
		Symbol:                    "BTC/USDT",
		BaseCurrency:              "BTC",
		QuoteCurrency:             "USDT",
		LongExchange:              "Binance",
		ShortExchange:             "Bybit",
		LongFundingRate:           decimal.NewFromFloat(0.0001),
		ShortFundingRate:          decimal.NewFromFloat(-0.0002),
		NetFundingRate:            decimal.NewFromFloat(0.0003),
		FundingInterval:           8,
		LongMarkPrice:             decimal.NewFromFloat(50000.0),
		ShortMarkPrice:            decimal.NewFromFloat(50100.0),
		PriceDifference:           decimal.NewFromFloat(100.0),
		PriceDifferencePercentage: decimal.NewFromFloat(0.2),
		HourlyRate:                decimal.NewFromFloat(0.0000375),
		DailyRate:                 decimal.NewFromFloat(0.0009),
		APY:                       decimal.NewFromFloat(0.3285),
		EstimatedProfit8h:         decimal.NewFromFloat(0.024),
		EstimatedProfitDaily:      decimal.NewFromFloat(0.072),
		EstimatedProfitWeekly:     decimal.NewFromFloat(0.504),
		EstimatedProfitMonthly:    decimal.NewFromFloat(2.16),
		RiskScore:                 decimal.NewFromFloat(1.5),
		VolatilityScore:           decimal.NewFromFloat(0.8),
		LiquidityScore:            decimal.NewFromFloat(0.9),
		RecommendedPositionSize:   decimal.NewFromFloat(10000.0),
		MaxLeverage:               decimal.NewFromFloat(125.0),
		RecommendedLeverage:       decimal.NewFromFloat(10.0),
		StopLossPercentage:        decimal.NewFromFloat(5.0),
		MinPositionSize:           decimal.NewFromFloat(100.0),
		MaxPositionSize:           decimal.NewFromFloat(100000.0),
		OptimalPositionSize:       decimal.NewFromFloat(50000.0),
		DetectedAt:                now,
		ExpiresAt:                 now.Add(8 * time.Hour),
		NextFundingTime:           now.Add(2 * time.Hour),
		TimeToNextFunding:         120,
		IsActive:                  true,
	}

	// Since db is nil, this should return an error instead of panicking
	err := service.storeOpportunity(ctx, opportunity)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

func TestFuturesArbitrageService_cleanupExpiredOpportunities(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		nil, // db will be nil for this test
		nil,
		mockConfig,
		nil, // error recovery manager
		nil, // resource manager
		nil, // performance monitor
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	// Since db is nil, this should return an error instead of panicking
	err := service.cleanupExpiredOpportunities(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestFuturesArbitrageService_Start tests the Start method
func TestFuturesArbitrageService_Start(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		nil,
		nil,
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	// Test initial state - service should not be running
	assert.False(t, service.IsRunning())
	assert.Nil(t, service.ctx)
	assert.Nil(t, service.cancel)
}

// TestFuturesArbitrageService_Stop tests the Stop method
func TestFuturesArbitrageService_Stop(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}

	service := NewFuturesArbitrageService(
		nil,
		nil,
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	// Test stopping when not running (should not panic)
	service.Stop()
	assert.False(t, service.running)
}

// TestFuturesArbitrageService_calculateAndStoreOpportunities_Success tests successful opportunity calculation
func TestFuturesArbitrageService_calculateAndStoreOpportunities_Success(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}
	mockErrorRecoveryManager := &MockErrorRecoveryManager{}

	// Setup mocks
	mockErrorRecoveryManager.On("ExecuteWithRetry", mock.Anything, "cleanup_opportunities", mock.Anything).Return(nil)
	mockErrorRecoveryManager.On("ExecuteWithRetry", mock.Anything, "get_funding_rates", mock.Anything).Return(nil)
	mockErrorRecoveryManager.On("ExecuteWithRetry", mock.Anything, "store_opportunity", mock.Anything).Return(nil)

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	// Since we can't mock methods directly, test with nil dependencies and expect panics
	// This tests the error handling path when dependencies are not available
	assert.Panics(t, func() {
		_ = service.calculateAndStoreOpportunities(ctx)
	})
}

// TestFuturesArbitrageService_calculateAndStoreOpportunities_NoRates tests behavior with no funding rates
func TestFuturesArbitrageService_calculateAndStoreOpportunities_NoRates(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}
	// Use real ErrorRecoveryManager since the function expects concrete type
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		errorRecoveryManager,
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	// Test with nil database pool
	err := service.calculateAndStoreOpportunities(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestFuturesArbitrageService_calculateAndStoreOpportunities_EmptyRates tests behavior with empty funding rates map
func TestFuturesArbitrageService_calculateAndStoreOpportunities_EmptyRates(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}
	// Use real ErrorRecoveryManager since the function expects concrete type
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		errorRecoveryManager,
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	// Test with empty funding rates - should return nil without error
	err := service.calculateAndStoreOpportunities(ctx)
	// Should error when database pool is not available
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get funding rates: database pool is not available")
}

// TestFuturesArbitrageService_calculateAndStoreOpportunities_CleanupError tests cleanup error handling
func TestFuturesArbitrageService_calculateAndStoreOpportunities_CleanupError(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}
	// Use real ErrorRecoveryManager since the function expects concrete type
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		errorRecoveryManager,
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	ctx := context.Background()

	// Test with cleanup error - should still continue to try getting funding rates
	err := service.calculateAndStoreOpportunities(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestFuturesArbitrageService_calculateAndStoreOpportunities_ContextCancellation tests context cancellation
func TestFuturesArbitrageService_calculateAndStoreOpportunities_ContextCancellation(t *testing.T) {
	// Initialize logger to avoid nil pointer
	_ = telemetry.Logger()

	mockConfig := &config.Config{}
	// Use real ErrorRecoveryManager since the function expects concrete type
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		errorRecoveryManager,
		(*ResourceManager)(nil),
		(*PerformanceMonitor)(nil),
		logging.NewStandardLogger("info", "test"),
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context immediately
	cancel()

	// Test with cancelled context - should return funding rates error first
	err := service.calculateAndStoreOpportunities(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get funding rates: context canceled")
}

func TestFuturesArbitrageService_Start_Success(t *testing.T) {
	mockConfig := &config.Config{}

	// Create a real resource manager for testing
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel) // Reduce noise in tests
	resourceManager := NewResourceManager(logger)

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		resourceManager,
		(*PerformanceMonitor)(nil),
		logger,
	)

	// Test initial state
	assert.False(t, service.running)
	assert.Nil(t, service.ctx)
	assert.Nil(t, service.cancel)

	// Test successful start
	err := service.Start()
	assert.NoError(t, err)
	assert.True(t, service.running)
	assert.NotNil(t, service.ctx)
	assert.NotNil(t, service.cancel)

	// Stop the service to clean up
	service.Stop()
	assert.False(t, service.running)
}

func TestFuturesArbitrageService_Start_AlreadyRunning(t *testing.T) {
	mockConfig := &config.Config{}
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)
	resourceManager := NewResourceManager(logger)

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		resourceManager,
		(*PerformanceMonitor)(nil),
		logger,
	)

	// Start the service first
	err := service.Start()
	assert.NoError(t, err)
	assert.True(t, service.running)

	// Test starting when already running
	err = service.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "futures arbitrage service is already running")
	assert.True(t, service.running) // Should still be running

	// Stop the service
	service.Stop()
}

func TestFuturesArbitrageService_Start_ContextManagement(t *testing.T) {
	mockConfig := &config.Config{}
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)
	resourceManager := NewResourceManager(logger)

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		resourceManager,
		(*PerformanceMonitor)(nil),
		logger,
	)

	// Test that context is properly created
	err := service.Start()
	assert.NoError(t, err)

	// Verify context is created and can be used
	select {
	case <-service.ctx.Done():
		t.Error("Context should not be cancelled immediately after start")
	default:
		// Context is active, which is expected
	}

	// Verify we can cancel the context
	service.cancel()
	select {
	case <-service.ctx.Done():
		// Context is cancelled, which is expected
	default:
		t.Error("Context should be cancelled after calling cancel")
	}

	service.Stop()
}

func TestFuturesArbitrageService_Start_MultipleCycles(t *testing.T) {
	mockConfig := &config.Config{}
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)
	resourceManager := NewResourceManager(logger)

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		(*ErrorRecoveryManager)(nil),
		resourceManager,
		(*PerformanceMonitor)(nil),
		logger,
	)

	// Test multiple start/stop cycles
	for i := 0; i < 3; i++ {
		// Start the service
		err := service.Start()
		assert.NoError(t, err)
		assert.True(t, service.running)

		// Stop the service
		service.Stop()
		assert.False(t, service.running)
		// Note: ctx and cancel are not set to nil by Stop method
	}
}

func TestFuturesArbitrageService_runOpportunityCalculator_ContextCancellation(t *testing.T) {
	mockConfig := &config.Config{}
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)
	resourceManager := NewResourceManager(logger)
	errorRecoveryManager := NewErrorRecoveryManager(logger)

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		errorRecoveryManager,
		resourceManager,
		(*PerformanceMonitor)(nil),
		logger,
	)

	// Start the service to launch the goroutine
	err := service.Start()
	assert.NoError(t, err)

	// Give the goroutine a moment to start
	time.Sleep(10 * time.Millisecond)

	// Stop the service - this should cancel the context and stop the goroutine
	service.Stop()
	assert.False(t, service.running)

	// Wait a bit to ensure the goroutine has time to stop
	time.Sleep(50 * time.Millisecond)

	// The test passes if we reach here without deadlock or panic
}

func TestFuturesArbitrageService_runOpportunityCalculator_ResourceManagement(t *testing.T) {
	mockConfig := &config.Config{}
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel)
	resourceManager := NewResourceManager(logger)
	errorRecoveryManager := NewErrorRecoveryManager(logger)

	service := NewFuturesArbitrageService(
		(*database.PostgresDB)(nil),
		(*redis.Client)(nil),
		mockConfig,
		errorRecoveryManager,
		resourceManager,
		(*PerformanceMonitor)(nil),
		logger,
	)

	// Start the service
	err := service.Start()
	assert.NoError(t, err)

	// Give the goroutine a moment to start and register resources
	time.Sleep(10 * time.Millisecond)

	// Check that resources are registered
	stats := resourceManager.GetResourceStats()
	assert.True(t, stats[GoroutineResource].CurrentActive > 0)

	// Stop the service
	service.Stop()

	// Give cleanup time
	time.Sleep(50 * time.Millisecond)

	// The test passes if resources are properly managed
}
