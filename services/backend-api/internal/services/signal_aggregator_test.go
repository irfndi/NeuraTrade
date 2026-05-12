package services

import (
	"context"
	"testing"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/irfndi/neuratrade/internal/models"
)

// MockSignalQualityScorer implements SignalQualityScorer interface for testing
type MockSignalQualityScorer struct {
	mock.Mock
}

func (m *MockSignalQualityScorer) AssessSignalQuality(ctx context.Context, input *SignalQualityInput) (*SignalQualityMetrics, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(*SignalQualityMetrics), args.Error(1)
}

func (m *MockSignalQualityScorer) IsSignalQualityAcceptable(metrics *SignalQualityMetrics, thresholds *QualityThresholds) bool {
	args := m.Called(metrics, thresholds)
	return args.Bool(0)
}

func (m *MockSignalQualityScorer) GetDefaultQualityThresholds() *QualityThresholds {
	args := m.Called()
	return args.Get(0).(*QualityThresholds)
}

// TestSignalAggregator_NewSignalAggregator tests the constructor
func TestSignalAggregator_NewSignalAggregator(t *testing.T) {
	logger := zaplogrus.New()

	// Test with nil config and db - should still create instance
	sa := NewSignalAggregator(nil, nil, logger)

	assert.NotNil(t, sa)
	assert.NotNil(t, sa.qualityScorer)
	assert.NotNil(t, sa.cache)
	assert.Equal(t, decimal.NewFromFloat(0.6), sa.sigConfig.MinConfidence)
	assert.Equal(t, decimal.NewFromFloat(0.5), sa.sigConfig.MinProfitThreshold)
}

func TestSignalAggregator_AggregateTechnicalSignals_AllowsMissingVolumeFallback(t *testing.T) {
	logger := zaplogrus.New()
	sa := NewSignalAggregator(nil, nil, logger)

	mockScorer := &MockSignalQualityScorer{}
	sa.qualityScorer = mockScorer

	qualityMetrics := &SignalQualityMetrics{
		OverallScore:         decimal.NewFromFloat(0.8),
		ExchangeScore:        decimal.NewFromFloat(0.8),
		VolumeScore:          decimal.NewFromFloat(0.5),
		LiquidityScore:       decimal.NewFromFloat(0.8),
		VolatilityScore:      decimal.NewFromFloat(0.7),
		TimingScore:          decimal.NewFromFloat(0.9),
		ConfidenceScore:      decimal.NewFromFloat(0.8),
		RiskScore:            decimal.NewFromFloat(0.2),
		DataFreshnessScore:   decimal.NewFromFloat(0.9),
		MarketConditionScore: decimal.NewFromFloat(0.8),
	}
	thresholds := &QualityThresholds{
		MinOverallScore:   decimal.NewFromFloat(0.6),
		MinExchangeScore:  decimal.NewFromFloat(0.7),
		MinVolumeScore:    decimal.NewFromFloat(0.5),
		MinLiquidityScore: decimal.NewFromFloat(0.6),
		MaxRiskScore:      decimal.NewFromFloat(0.4),
		MinDataFreshness:  5 * time.Minute,
	}
	mockScorer.On("AssessSignalQuality", mock.Anything, mock.MatchedBy(func(input *SignalQualityInput) bool {
		return input != nil && input.VolumeUnavailable && input.Volume.IsZero()
	})).Return(qualityMetrics, nil).Once()
	mockScorer.On("GetDefaultQualityThresholds").Return(thresholds).Once()
	mockScorer.On("IsSignalQualityAcceptable", qualityMetrics, thresholds).Return(true).Once()

	prices := make([]decimal.Decimal, 60)
	for i := range prices {
		prices[i] = decimal.NewFromInt(int64(100 + i))
	}

	signals, err := sa.AggregateTechnicalSignals(context.Background(), TechnicalSignalInput{
		Symbol:   "BTC/USDT",
		Exchange: "bitget",
		Prices:   prices,
		Volumes:  nil,
	})

	require.NoError(t, err)
	require.NotEmpty(t, signals)
	assert.Equal(t, "BTC/USDT", signals[0].Symbol)
	assert.Equal(t, SignalTypeTechnical, signals[0].SignalType)
	mockScorer.AssertExpectations(t)
}

func TestSignalAggregator_AggregateTechnicalSignals_TreatsSparseVolumeAsUnavailableForQuality(t *testing.T) {
	logger := zaplogrus.New()
	sa := NewSignalAggregator(nil, nil, logger)

	mockScorer := &MockSignalQualityScorer{}
	sa.qualityScorer = mockScorer

	qualityMetrics := &SignalQualityMetrics{
		OverallScore:         decimal.NewFromFloat(0.8),
		ExchangeScore:        decimal.NewFromFloat(0.8),
		VolumeScore:          decimal.NewFromFloat(missingVolumeNeutralScore),
		LiquidityScore:       decimal.NewFromFloat(0.8),
		VolatilityScore:      decimal.NewFromFloat(0.7),
		TimingScore:          decimal.NewFromFloat(0.9),
		ConfidenceScore:      decimal.NewFromFloat(0.8),
		RiskScore:            decimal.NewFromFloat(0.2),
		DataFreshnessScore:   decimal.NewFromFloat(0.9),
		MarketConditionScore: decimal.NewFromFloat(0.8),
	}
	thresholds := &QualityThresholds{
		MinOverallScore:   decimal.NewFromFloat(0.6),
		MinExchangeScore:  decimal.NewFromFloat(0.7),
		MinVolumeScore:    decimal.NewFromFloat(0.5),
		MinLiquidityScore: decimal.NewFromFloat(0.6),
		MaxRiskScore:      decimal.NewFromFloat(0.4),
		MinDataFreshness:  5 * time.Minute,
	}
	mockScorer.On("AssessSignalQuality", mock.Anything, mock.MatchedBy(func(input *SignalQualityInput) bool {
		return input != nil && input.VolumeUnavailable && input.Volume.Equal(decimal.NewFromInt(100))
	})).Return(qualityMetrics, nil).Once()
	mockScorer.On("GetDefaultQualityThresholds").Return(thresholds).Once()
	mockScorer.On("IsSignalQualityAcceptable", qualityMetrics, thresholds).Return(true).Once()

	prices := make([]decimal.Decimal, 60)
	for i := range prices {
		prices[i] = decimal.NewFromInt(int64(100 + i))
	}
	volumes := make([]decimal.Decimal, 10)
	for i := range volumes {
		volumes[i] = decimal.NewFromInt(100)
	}

	signals, err := sa.AggregateTechnicalSignals(context.Background(), TechnicalSignalInput{
		Symbol:   "BTC/USDT",
		Exchange: "bitget",
		Prices:   prices,
		Volumes:  volumes,
	})

	require.NoError(t, err)
	require.NotEmpty(t, signals)
	mockScorer.AssertExpectations(t)
}

// TestSignalAggregator_AggregateArbitrageSignals_Basic tests basic signal aggregation
func TestSignalAggregator_AggregateArbitrageSignals_Basic(t *testing.T) {
	logger := zaplogrus.New()
	sa := NewSignalAggregator(nil, nil, logger)

	// Replace the QualityScorer with a mock for testing
	mockScorer := &MockSignalQualityScorer{}
	sa.qualityScorer = mockScorer

	// Configure mock to return acceptable quality metrics
	qualityMetrics := &SignalQualityMetrics{
		OverallScore:         decimal.NewFromFloat(0.8),
		ExchangeScore:        decimal.NewFromFloat(0.8),
		VolumeScore:          decimal.NewFromFloat(0.8),
		LiquidityScore:       decimal.NewFromFloat(0.8),
		VolatilityScore:      decimal.NewFromFloat(0.7),
		TimingScore:          decimal.NewFromFloat(0.9),
		ConfidenceScore:      decimal.NewFromFloat(0.8),
		RiskScore:            decimal.NewFromFloat(0.2),
		DataFreshnessScore:   decimal.NewFromFloat(0.9),
		MarketConditionScore: decimal.NewFromFloat(0.8),
	}

	thresholds := &QualityThresholds{
		MinOverallScore:   decimal.NewFromFloat(0.6),
		MinExchangeScore:  decimal.NewFromFloat(0.7),
		MinVolumeScore:    decimal.NewFromFloat(0.5),
		MinLiquidityScore: decimal.NewFromFloat(0.6),
		MaxRiskScore:      decimal.NewFromFloat(0.4),
		MinDataFreshness:  5 * time.Minute,
	}

	mockScorer.On("AssessSignalQuality", mock.Anything, mock.Anything).Return(qualityMetrics, nil)
	mockScorer.On("IsSignalQualityAcceptable", qualityMetrics, thresholds).Return(true)
	mockScorer.On("GetDefaultQualityThresholds").Return(thresholds)

	// Create test opportunities
	now := time.Now()
	opportunities := []models.ArbitrageOpportunity{
		{
			ID:               "test-1",
			TradingPairID:    1,
			BuyExchangeID:    1,
			SellExchangeID:   2,
			BuyPrice:         decimal.NewFromFloat(50000.0),
			SellPrice:        decimal.NewFromFloat(51000.0), // 2.0% profit
			ProfitPercentage: decimal.NewFromFloat(2.0),     // 2.0%
			DetectedAt:       now,
			ExpiresAt:        now.Add(5 * time.Minute),
			TradingPair:      &models.TradingPair{Symbol: "BTC/USDT"},
		},
		{
			ID:               "test-2",
			TradingPairID:    1,
			BuyExchangeID:    1,
			SellExchangeID:   2,
			BuyPrice:         decimal.NewFromFloat(50100.0),
			SellPrice:        decimal.NewFromFloat(51600.0), // 3.0% profit
			ProfitPercentage: decimal.NewFromFloat(3.0),     // 3.0%
			DetectedAt:       now,
			ExpiresAt:        now.Add(5 * time.Minute),
			TradingPair:      &models.TradingPair{Symbol: "BTC/USDT"},
		},
	}

	input := ArbitrageSignalInput{
		Opportunities: opportunities,
		MinVolume:     decimal.NewFromFloat(10000),
		BaseAmount:    decimal.NewFromFloat(20000),
	}

	signals, err := sa.AggregateArbitrageSignals(context.Background(), input)

	assert.NoError(t, err)
	// Should get signals since profit percentage >= 0.5% threshold
	assert.NotEmpty(t, signals)

	if len(signals) > 0 {
		signal := signals[0]
		assert.Equal(t, SignalTypeArbitrage, signal.SignalType)
		assert.True(t, signal.Confidence.GreaterThanOrEqual(decimal.NewFromFloat(0.6)))
		assert.True(t, signal.ProfitPotential.GreaterThanOrEqual(decimal.NewFromFloat(2.0)))
		assert.False(t, signal.CreatedAt.IsZero())
		assert.False(t, signal.ExpiresAt.IsZero())
	}
}

// TestSignalAggregator_AggregateArbitrageSignals_EmptyInput tests empty input handling
func TestSignalAggregator_AggregateArbitrageSignals_EmptyInput(t *testing.T) {
	logger := zaplogrus.New()
	sa := NewSignalAggregator(nil, nil, logger)

	input := ArbitrageSignalInput{
		Opportunities: []models.ArbitrageOpportunity{},
		MinVolume:     decimal.NewFromFloat(10000),
		BaseAmount:    decimal.NewFromFloat(20000),
	}

	signals, err := sa.AggregateArbitrageSignals(context.Background(), input)

	assert.NoError(t, err)
	assert.Empty(t, signals)
}

// TestSignalAggregator_AggregateArbitrageSignals_LowProfit tests low profit filtering
func TestSignalAggregator_AggregateArbitrageSignals_LowProfit(t *testing.T) {
	logger := zaplogrus.New()
	sa := NewSignalAggregator(nil, nil, logger)

	// Create test opportunities with low profit percentage
	now := time.Now()
	opportunities := []models.ArbitrageOpportunity{
		{
			ID:               "test-1",
			TradingPairID:    1,
			BuyExchangeID:    1,
			SellExchangeID:   2,
			BuyPrice:         decimal.NewFromFloat(50000.0),
			SellPrice:        decimal.NewFromFloat(50200.0),
			ProfitPercentage: decimal.NewFromFloat(0.4), // 0.4% (below 0.5% threshold)
			DetectedAt:       now,
			ExpiresAt:        now.Add(5 * time.Minute),
			TradingPair:      &models.TradingPair{Symbol: "BTC/USDT"},
		},
	}

	input := ArbitrageSignalInput{
		Opportunities: opportunities,
		MinVolume:     decimal.NewFromFloat(10000),
		BaseAmount:    decimal.NewFromFloat(20000),
	}

	signals, err := sa.AggregateArbitrageSignals(context.Background(), input)

	assert.NoError(t, err)
	// Should get no signals since profit percentage is below 0.5% threshold
	assert.Empty(t, signals)
}

// TestSignalAggregator_AggregateArbitrageSignals_QualityFilter tests quality filtering
func TestSignalAggregator_AggregateArbitrageSignals_QualityFilter(t *testing.T) {
	logger := zaplogrus.New()
	sa := NewSignalAggregator(nil, nil, logger)

	// Create test opportunities with good profit but poor quality
	now := time.Now()
	opportunities := []models.ArbitrageOpportunity{
		{
			ID:               "test-1",
			TradingPairID:    1,
			BuyExchangeID:    1,
			SellExchangeID:   2,
			BuyPrice:         decimal.NewFromFloat(50000.0),
			SellPrice:        decimal.NewFromFloat(51000.0),
			ProfitPercentage: decimal.NewFromFloat(2.0), // 2.0% (good profit)
			DetectedAt:       now,
			ExpiresAt:        now.Add(5 * time.Minute),
			TradingPair:      &models.TradingPair{Symbol: "BTC/USDT"},
		},
	}

	input := ArbitrageSignalInput{
		Opportunities: opportunities,
		MinVolume:     decimal.NewFromFloat(10000),
		BaseAmount:    decimal.NewFromFloat(20000),
	}

	signals, err := sa.AggregateArbitrageSignals(context.Background(), input)

	assert.NoError(t, err)
	// Should get no signals since quality is unacceptable
	assert.Empty(t, signals)
}

// TestSignalAggregator_AggregateArbitrageSignals_MultipleOpportunities tests multiple opportunity handling
func TestSignalAggregator_AggregateArbitrageSignals_MultipleOpportunities(t *testing.T) {
	logger := zaplogrus.New()
	sa := NewSignalAggregator(nil, nil, logger)

	// Replace the QualityScorer with a mock for testing
	mockScorer := &MockSignalQualityScorer{}
	sa.qualityScorer = mockScorer

	// Configure mock to return acceptable quality metrics
	qualityMetrics := &SignalQualityMetrics{
		OverallScore:         decimal.NewFromFloat(0.8),
		ExchangeScore:        decimal.NewFromFloat(0.8),
		VolumeScore:          decimal.NewFromFloat(0.8),
		LiquidityScore:       decimal.NewFromFloat(0.8),
		VolatilityScore:      decimal.NewFromFloat(0.7),
		TimingScore:          decimal.NewFromFloat(0.9),
		ConfidenceScore:      decimal.NewFromFloat(0.8),
		RiskScore:            decimal.NewFromFloat(0.2),
		DataFreshnessScore:   decimal.NewFromFloat(0.9),
		MarketConditionScore: decimal.NewFromFloat(0.8),
	}

	thresholds := &QualityThresholds{
		MinOverallScore:   decimal.NewFromFloat(0.6),
		MinExchangeScore:  decimal.NewFromFloat(0.7),
		MinVolumeScore:    decimal.NewFromFloat(0.5),
		MinLiquidityScore: decimal.NewFromFloat(0.6),
		MaxRiskScore:      decimal.NewFromFloat(0.4),
		MinDataFreshness:  5 * time.Minute,
	}

	mockScorer.On("AssessSignalQuality", mock.Anything, mock.Anything).Return(qualityMetrics, nil)
	mockScorer.On("IsSignalQualityAcceptable", qualityMetrics, thresholds).Return(true)
	mockScorer.On("GetDefaultQualityThresholds").Return(thresholds)

	// Create test opportunities with different profit levels
	now := time.Now()
	opportunities := []models.ArbitrageOpportunity{
		{
			ID:               "test-1",
			TradingPairID:    1,
			BuyExchangeID:    1,
			SellExchangeID:   2,
			BuyPrice:         decimal.NewFromFloat(50000.0),
			SellPrice:        decimal.NewFromFloat(51000.0), // 2.0%
			ProfitPercentage: decimal.NewFromFloat(2.0),     // 2.0%
			DetectedAt:       now,
			ExpiresAt:        now.Add(5 * time.Minute),
			TradingPair:      &models.TradingPair{Symbol: "BTC/USDT"},
		},
		{
			ID:               "test-2",
			TradingPairID:    1,
			BuyExchangeID:    1,
			SellExchangeID:   2,
			BuyPrice:         decimal.NewFromFloat(50100.0),
			SellPrice:        decimal.NewFromFloat(51600.0), // 3.0%
			ProfitPercentage: decimal.NewFromFloat(3.0),     // 3.0%
			DetectedAt:       now,
			ExpiresAt:        now.Add(5 * time.Minute),
			TradingPair:      &models.TradingPair{Symbol: "BTC/USDT"},
		},
		{
			ID:               "test-3",
			TradingPairID:    1,
			BuyExchangeID:    1,
			SellExchangeID:   2,
			BuyPrice:         decimal.NewFromFloat(50200.0),
			SellPrice:        decimal.NewFromFloat(52200.0), // 4.0%
			ProfitPercentage: decimal.NewFromFloat(4.0),     // 4.0%
			DetectedAt:       now,
			ExpiresAt:        now.Add(5 * time.Minute),
			TradingPair:      &models.TradingPair{Symbol: "BTC/USDT"},
		},
	}

	input := ArbitrageSignalInput{
		Opportunities: opportunities,
		MinVolume:     decimal.NewFromFloat(10000),
		BaseAmount:    decimal.NewFromFloat(20000),
	}

	signals, err := sa.AggregateArbitrageSignals(context.Background(), input)

	assert.NoError(t, err)
	// Should get signals since some opportunities meet the threshold
	assert.NotEmpty(t, signals)

	// Verify signal properties
	if len(signals) > 0 {
		signal := signals[0]
		assert.Equal(t, SignalTypeArbitrage, signal.SignalType)
		assert.True(t, signal.Confidence.GreaterThanOrEqual(decimal.NewFromFloat(0.6)))
		assert.True(t, signal.ProfitPotential.GreaterThanOrEqual(decimal.NewFromFloat(2.0)))
		assert.NotEmpty(t, signal.Exchanges)
	}
}
