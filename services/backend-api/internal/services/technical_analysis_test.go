package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
)

// MockTechnicalDatabase is a mock implementation of the database for technical analysis
type MockTechnicalDatabase struct {
	mock.Mock
	PostgresDB *MockGormDB
}

// MockGormDB is a mock implementation of GORM DB
type MockGormDB struct {
	mock.Mock
}

func (m *MockGormDB) WithContext(ctx context.Context) *MockGormDB {
	m.Called(ctx)
	return m
}

func (m *MockGormDB) Where(query interface{}, args ...interface{}) *MockGormDB {
	m.Called(query, args)
	return m
}

func (m *MockGormDB) Order(value interface{}) *MockGormDB {
	m.Called(value)
	return m
}

func (m *MockGormDB) Limit(limit int) *MockGormDB {
	m.Called(limit)
	return m
}

func (m *MockGormDB) Find(dest interface{}, conds ...interface{}) *MockGormDB {
	m.Called(dest, conds)
	return &MockGormDB{Mock: mock.Mock{}}
}

func (m *MockGormDB) Error() error {
	args := m.Called()
	return args.Error(0)
}

// Test data generators

func generateTestPriceData(count int) *PriceData {
	priceData := &PriceData{
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Open:       make([]decimal.Decimal, count),
		High:       make([]decimal.Decimal, count),
		Low:        make([]decimal.Decimal, count),
		Close:      make([]decimal.Decimal, count),
		Volume:     make([]decimal.Decimal, count),
		Timestamps: make([]time.Time, count),
	}

	basePrice := 50000.0
	baseTime := time.Now().Add(-time.Duration(count) * time.Hour)

	for i := 0; i < count; i++ {
		// Generate realistic price movement
		price := basePrice + float64(i)*10 + float64(i%10)*5
		volume := 100.0 + float64(i%20)*10

		priceData.Open[i] = decimal.NewFromFloat(price - 5)
		priceData.High[i] = decimal.NewFromFloat(price + 10)
		priceData.Low[i] = decimal.NewFromFloat(price - 10)
		priceData.Close[i] = decimal.NewFromFloat(price)
		priceData.Volume[i] = decimal.NewFromFloat(volume)
		priceData.Timestamps[i] = baseTime.Add(time.Duration(i) * time.Hour)
	}

	return priceData
}

// Test suite setup

func setupTestService() (*TechnicalAnalysisService, *MockTechnicalDatabase) {
	cfg := &config.Config{}
	logger := zaplogrus.New()
	logger.SetLevel(zaplogrus.ErrorLevel) // Reduce noise in tests

	mockDB := &MockTechnicalDatabase{
		PostgresDB: &MockGormDB{},
	}

	service := &TechnicalAnalysisService{
		config: cfg,
		db:     &database.PostgresDB{}, // Will be mocked
		logger: logger,
	}

	return service, mockDB
}

// Unit Tests

func TestNewTechnicalAnalysisService(t *testing.T) {
	cfg := &config.Config{}
	db := &database.PostgresDB{}
	logger := zaplogrus.New()

	// Create required dependencies
	errorRecoveryManager := NewErrorRecoveryManager(logger)
	resourceManager := NewResourceManager(logger)
	performanceMonitor := NewPerformanceMonitor(logger, nil, context.Background())

	service := NewTechnicalAnalysisService(cfg, db, logger, errorRecoveryManager, resourceManager, performanceMonitor)

	assert.NotNil(t, service)
	assert.Equal(t, cfg, service.config)
	assert.Equal(t, db, service.db)
	assert.Equal(t, logger, service.logger)
}

func TestGetDefaultIndicatorConfig(t *testing.T) {
	service, _ := setupTestService()

	config := service.GetDefaultIndicatorConfig()

	assert.NotNil(t, config)
	assert.Equal(t, []int{10, 20, 50}, config.SMAPeriods)
	assert.Equal(t, []int{12, 26}, config.EMAPeriods)
	assert.Equal(t, 14, config.RSIPeriod)
	assert.Equal(t, 12, config.MACDFast)
	assert.Equal(t, 26, config.MACDSlow)
	assert.Equal(t, 9, config.MACDSignal)
	assert.Equal(t, 20, config.BBPeriod)
	assert.Equal(t, 2.0, config.BBStdDev)
	assert.Equal(t, 14, config.ATRPeriod)
	assert.True(t, config.OBVEnabled)
	assert.True(t, config.VWAPEnabled)
}

func TestCalculateSMA(t *testing.T) {
	t.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()

	tests := []struct {
		name     string
		prices   []float64
		period   int
		expected bool // whether result should be non-nil
	}{
		{
			name:     "Valid SMA calculation",
			prices:   []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
			period:   5,
			expected: true,
		},
		{
			name:     "Insufficient data",
			prices:   []float64{10, 11, 12},
			period:   5,
			expected: false,
		},
		{
			name:     "Empty prices",
			prices:   []float64{},
			period:   5,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.calculateSMA(tt.prices, tt.period)

			if tt.expected {
				assert.NotNil(t, result)
				assert.Equal(t, fmt.Sprintf("SMA_%d", tt.period), result.Name)
				assert.NotEmpty(t, result.Values)
				assert.Contains(t, []string{"buy", "sell", "hold"}, result.Signal)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestCalculateEMA(t *testing.T) {
	t.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()

	prices := []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	period := 5

	result := service.calculateEMA(prices, period)

	assert.NotNil(t, result)
	assert.Equal(t, fmt.Sprintf("EMA_%d", period), result.Name)
	assert.NotEmpty(t, result.Values)
	assert.Contains(t, []string{"buy", "sell", "hold"}, result.Signal)
}

func TestCalculateRSI(t *testing.T) {
	t.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()

	tests := []struct {
		name     string
		prices   []float64
		period   int
		expected bool
	}{
		{
			name:     "Valid RSI calculation",
			prices:   []float64{44, 44.34, 44.09, 44.15, 43.61, 44.33, 44.83, 45.85, 47.25, 47.92, 46.23, 44.18, 46.57, 46.61, 45.41},
			period:   14,
			expected: true,
		},
		{
			name:     "Insufficient data",
			prices:   []float64{44, 44.34, 44.09},
			period:   14,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.calculateRSI(tt.prices, tt.period)

			if tt.expected {
				assert.NotNil(t, result)
				assert.Equal(t, fmt.Sprintf("RSI_%d", tt.period), result.Name)
				assert.NotEmpty(t, result.Values)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestCalculateMACD(t *testing.T) {
	t.Skip("Skipping due to hangs in github.com/cinar/indicator/v2 MACD implementation")

	service, _ := setupTestService()

	prices := make([]float64, 200)
	for i := range prices {
		prices[i] = 100 + float64(i)*0.5 // Trending upward
	}

	result := service.calculateMACD(prices, 12, 26, 9)

	assert.NotNil(t, result)
	assert.Equal(t, "MACD", result.Name)
	assert.NotEmpty(t, result.Values)
	assert.Contains(t, []string{"buy", "sell", "hold"}, result.Signal)
}

func TestCalculateBollingerBands(t *testing.T) {
	t.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()

	prices := make([]float64, 30)
	for i := range prices {
		prices[i] = 100 + float64(i%10) // Oscillating pattern
	}

	result := service.calculateBollingerBands(prices, 20, 2.0)

	assert.NotNil(t, result)
	assert.Equal(t, "BB", result.Name)
	assert.NotEmpty(t, result.Values)
	assert.Contains(t, []string{"buy", "sell", "hold"}, result.Signal)
}

func TestCalculateATR(t *testing.T) {
	t.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()

	count := 30
	high := make([]float64, count)
	low := make([]float64, count)
	close := make([]float64, count)

	for i := 0; i < count; i++ {
		base := 100.0 + float64(i)*0.5
		high[i] = base + 2
		low[i] = base - 2
		close[i] = base
	}

	result := service.calculateATR(high, low, close, 14)

	assert.NotNil(t, result)
	assert.Equal(t, "ATR_14", result.Name)
	assert.NotEmpty(t, result.Values)
	assert.Equal(t, "hold", result.Signal) // ATR is for volatility measurement
}

func TestCalculateStochastic(t *testing.T) {
	t.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()

	count := 30
	high := make([]float64, count)
	low := make([]float64, count)
	close := make([]float64, count)

	for i := 0; i < count; i++ {
		base := 100.0 + float64(i%20)
		high[i] = base + 5
		low[i] = base - 5
		close[i] = base
	}

	result := service.calculateStochastic(high, low, close, 14, 3)

	assert.NotNil(t, result)
	assert.Equal(t, "STOCH", result.Name)
	assert.NotEmpty(t, result.Values)
	assert.Contains(t, []string{"buy", "sell", "hold"}, result.Signal)
}

func TestCalculateOBV(t *testing.T) {
	t.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()

	prices := []float64{100, 101, 102, 101, 100, 99, 100, 101}
	volumes := []float64{1000, 1100, 1200, 900, 800, 700, 1000, 1100}

	result := service.calculateOBV(prices, volumes)

	assert.NotNil(t, result)
	assert.Equal(t, "OBV", result.Name)
	assert.NotEmpty(t, result.Values)
	assert.Contains(t, []string{"buy", "sell", "hold"}, result.Signal)
}

// Signal Analysis Tests

func TestAnalyzeSMASignal(t *testing.T) {
	service, _ := setupTestService()

	tests := []struct {
		name           string
		prices         []float64
		sma            []float64
		expectedSignal string
	}{
		{
			name:           "Price crossing above SMA",
			prices:         []float64{99, 101},
			sma:            []float64{100, 100},
			expectedSignal: "buy",
		},
		{
			name:           "Price crossing below SMA",
			prices:         []float64{101, 99},
			sma:            []float64{100, 100},
			expectedSignal: "sell",
		},
		{
			name:           "Price above SMA",
			prices:         []float64{101, 102},
			sma:            []float64{100, 100},
			expectedSignal: "buy",
		},
		{
			name:           "Price below SMA",
			prices:         []float64{99, 98},
			sma:            []float64{100, 100},
			expectedSignal: "sell",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal, strength := service.analyzeSMASignal(tt.prices, tt.sma, 10)
			assert.Equal(t, tt.expectedSignal, signal)
			assert.True(t, strength.GreaterThan(decimal.Zero))
		})
	}
}

func TestAnalyzeRSISignal(t *testing.T) {
	service, _ := setupTestService()

	tests := []struct {
		name           string
		rsi            []float64
		expectedSignal string
	}{
		{
			name:           "Oversold condition",
			rsi:            []float64{25},
			expectedSignal: "buy",
		},
		{
			name:           "Overbought condition",
			rsi:            []float64{75},
			expectedSignal: "sell",
		},
		{
			name:           "Neutral condition",
			rsi:            []float64{50},
			expectedSignal: "hold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal, strength := service.analyzeRSISignal(tt.rsi)
			assert.Equal(t, tt.expectedSignal, signal)
			assert.True(t, strength.GreaterThan(decimal.Zero))
		})
	}
}

func TestAnalyzeMACDSignal(t *testing.T) {
	service, _ := setupTestService()

	tests := []struct {
		name           string
		macd           []float64
		expectedSignal string
	}{
		{
			name:           "MACD crossing above zero",
			macd:           []float64{-0.1, 0.1},
			expectedSignal: "buy",
		},
		{
			name:           "MACD crossing below zero",
			macd:           []float64{0.1, -0.1},
			expectedSignal: "sell",
		},
		{
			name:           "MACD above zero",
			macd:           []float64{0.1, 0.2},
			expectedSignal: "buy",
		},
		{
			name:           "MACD below zero",
			macd:           []float64{-0.1, -0.2},
			expectedSignal: "sell",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal, strength := service.analyzeMACDSignal(tt.macd)
			assert.Equal(t, tt.expectedSignal, signal)
			assert.True(t, strength.GreaterThan(decimal.Zero))
		})
	}
}

func TestDetermineOverallSignal(t *testing.T) {
	service, _ := setupTestService()

	tests := []struct {
		name           string
		indicators     []*IndicatorResult
		expectedSignal string
	}{
		{
			name: "Strong buy signals",
			indicators: []*IndicatorResult{
				{Signal: "buy", Strength: decimal.NewFromFloat(0.8)},
				{Signal: "buy", Strength: decimal.NewFromFloat(0.7)},
				{Signal: "hold", Strength: decimal.NewFromFloat(0.5)},
			},
			expectedSignal: "buy",
		},
		{
			name: "Strong sell signals",
			indicators: []*IndicatorResult{
				{Signal: "sell", Strength: decimal.NewFromFloat(0.8)},
				{Signal: "sell", Strength: decimal.NewFromFloat(0.7)},
				{Signal: "hold", Strength: decimal.NewFromFloat(0.5)},
			},
			expectedSignal: "sell",
		},
		{
			name: "Mixed signals",
			indicators: []*IndicatorResult{
				{Signal: "buy", Strength: decimal.NewFromFloat(0.6)},
				{Signal: "sell", Strength: decimal.NewFromFloat(0.6)},
				{Signal: "hold", Strength: decimal.NewFromFloat(0.5)},
			},
			expectedSignal: "hold",
		},
		{
			name:           "No indicators",
			indicators:     []*IndicatorResult{},
			expectedSignal: "hold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal, confidence := service.determineOverallSignal(tt.indicators)
			assert.Equal(t, tt.expectedSignal, signal)
			assert.True(t, confidence.GreaterThanOrEqual(decimal.Zero))
			assert.True(t, confidence.LessThanOrEqual(decimal.NewFromFloat(1.0)))
		})
	}
}

// Helper function tests

func TestConvertToSnapshots(t *testing.T) {
	service, _ := setupTestService()
	priceData := generateTestPriceData(10)

	snapshots := service.convertToSnapshots(priceData)

	assert.Len(t, snapshots, 10)
	for i, snapshot := range snapshots {
		assert.Equal(t, priceData.Timestamps[i], snapshot.Date)
		open, _ := priceData.Open[i].Float64()
		assert.Equal(t, open, snapshot.Open)
		high, _ := priceData.High[i].Float64()
		assert.Equal(t, high, snapshot.High)
		low, _ := priceData.Low[i].Float64()
		assert.Equal(t, low, snapshot.Low)
		close, _ := priceData.Close[i].Float64()
		assert.Equal(t, close, snapshot.Close)
		volume, _ := priceData.Volume[i].Float64()
		assert.Equal(t, volume, snapshot.Volume)
	}
}

// Integration-style tests

func TestCalculateAllIndicators(t *testing.T) {
	t.Skip("Skipping due to hangs in MACD calculation")

	service, _ := setupTestService()
	priceData := generateTestPriceData(100)
	snapshots := service.convertToSnapshots(priceData)
	config := service.GetDefaultIndicatorConfig()

	indicators := service.calculateAllIndicators(snapshots, config)

	assert.NotEmpty(t, indicators)

	// Check that we have indicators for each configured type
	indicatorNames := make(map[string]bool)
	for _, indicator := range indicators {
		indicatorNames[indicator.Name] = true
	}

	// Should have SMA indicators
	for _, period := range config.SMAPeriods {
		assert.True(t, indicatorNames[fmt.Sprintf("SMA_%d", period)])
	}

	// Should have EMA indicators
	for _, period := range config.EMAPeriods {
		assert.True(t, indicatorNames[fmt.Sprintf("EMA_%d", period)])
	}

	// Should have other indicators
	assert.True(t, indicatorNames[fmt.Sprintf("RSI_%d", config.RSIPeriod)])
	assert.True(t, indicatorNames["MACD"])
	assert.True(t, indicatorNames["BB"])
	assert.True(t, indicatorNames[fmt.Sprintf("ATR_%d", config.ATRPeriod)])
	assert.True(t, indicatorNames["STOCH"])
	if config.OBVEnabled {
		assert.True(t, indicatorNames["OBV"])
	}
}

// Benchmark tests

func BenchmarkCalculateSMA(b *testing.B) {
	b.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()
	prices := make([]float64, 1000)
	for i := range prices {
		prices[i] = 100 + float64(i)*0.1
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.calculateSMA(prices, 20)
	}
}

func BenchmarkCalculateRSI(b *testing.B) {
	b.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()
	prices := make([]float64, 1000)
	for i := range prices {
		prices[i] = 100 + float64(i)*0.1
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.calculateRSI(prices, 14)
	}
}

func BenchmarkCalculateAllIndicators(b *testing.B) {
	b.Skip("Skipping due to hangs in MACD calculation")

	service, _ := setupTestService()
	priceData := generateTestPriceData(200)
	snapshots := service.convertToSnapshots(priceData)
	config := service.GetDefaultIndicatorConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.calculateAllIndicators(snapshots, config)
	}
}

// Error handling tests

func TestCalculateIndicatorsWithInsufficientData(t *testing.T) {
	t.Skip("Skipping due to goroutine leaks in github.com/cinar/indicator/v2 library")

	service, _ := setupTestService()

	// Test with very small dataset
	prices := []float64{100, 101}

	// These should return nil due to insufficient data
	assert.Nil(t, service.calculateSMA(prices, 10))
	assert.Nil(t, service.calculateEMA(prices, 10))
	assert.Nil(t, service.calculateRSI(prices, 14))
	assert.Nil(t, service.calculateMACD(prices, 12, 26, 9))
}

func TestAnalyzeSignalsWithEmptyData(t *testing.T) {
	service, _ := setupTestService()

	// Test signal analysis with empty data
	signal, strength := service.analyzeSMASignal([]float64{}, []float64{}, 10)
	assert.Equal(t, "hold", signal)
	assert.Equal(t, decimal.NewFromFloat(0.5), strength)

	signal, strength = service.analyzeRSISignal([]float64{})
	assert.Equal(t, "hold", signal)
	assert.Equal(t, decimal.NewFromFloat(0.5), strength)

	signal, strength = service.analyzeMACDSignal([]float64{})
	assert.Equal(t, "hold", signal)
	assert.Equal(t, decimal.NewFromFloat(0.5), strength)
}
