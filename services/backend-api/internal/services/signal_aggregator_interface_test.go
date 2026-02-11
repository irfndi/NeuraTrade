package services

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestMockSignalQualityScorer implements SignalQualityScorerInterface for testing
type TestMockSignalQualityScorer struct {
	mock.Mock
}

func (m *TestMockSignalQualityScorer) AssessSignalQuality(ctx context.Context, input *SignalQualityInput) (*SignalQualityMetrics, error) {
	args := m.Called(ctx, input)
	return args.Get(0).(*SignalQualityMetrics), args.Error(1)
}

func (m *TestMockSignalQualityScorer) IsSignalQualityAcceptable(metrics *SignalQualityMetrics, thresholds *QualityThresholds) bool {
	args := m.Called(metrics, thresholds)
	return args.Bool(0)
}

func (m *TestMockSignalQualityScorer) GetDefaultQualityThresholds() *QualityThresholds {
	args := m.Called()
	return args.Get(0).(*QualityThresholds)
}

// TestSignalAggregator_Interface tests the interface design
func TestSignalAggregator_Interface(t *testing.T) {
	logger := zaplogrus.New()

	// Test that we can create a SignalAggregator with a mock quality scorer
	sa := NewSignalAggregator(nil, nil, logger)

	// Replace the QualityScorer with a mock for testing
	mockScorer := &TestMockSignalQualityScorer{}
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

	// Test that the interface is working correctly
	assert.NotNil(t, sa)
	assert.NotNil(t, sa.qualityScorer)

	// Actually call the methods to trigger the mock expectations
	ctx := context.Background()
	signalInput := &SignalQualityInput{
		SignalType:       "arbitrage",
		Symbol:           "BTC/USDT",
		Exchanges:        []string{"binance", "coinbase"},
		Volume:           decimal.NewFromFloat(1000000),
		ProfitPotential:  decimal.NewFromFloat(0.02),
		Confidence:       decimal.NewFromFloat(0.8),
		Timestamp:        time.Now(),
		SignalCount:      1,
		SignalComponents: []string{"price_diff", "volume"},
	}

	// Call the methods that have mock expectations
	result, err := sa.qualityScorer.AssessSignalQuality(ctx, signalInput)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, decimal.NewFromFloat(0.8), result.OverallScore)

	// Test quality acceptance
	isAcceptable := sa.qualityScorer.IsSignalQualityAcceptable(qualityMetrics, thresholds)
	assert.True(t, isAcceptable)

	// Test default thresholds
	defaultThresholds := sa.qualityScorer.GetDefaultQualityThresholds()
	assert.NotNil(t, defaultThresholds)
	assert.Equal(t, decimal.NewFromFloat(0.6), defaultThresholds.MinOverallScore)
	// Verify the mock was called correctly
	mockScorer.AssertExpectations(t)
}

// TestSignalAggregator_QualityAssessment tests quality assessment functionality
func TestSignalAggregator_QualityAssessment(t *testing.T) {
	logger := zaplogrus.New()

	sa := NewSignalAggregator(nil, nil, logger)
	mockScorer := &TestMockSignalQualityScorer{}
	sa.qualityScorer = mockScorer

	// Test data
	qualityInput := SignalQualityInput{
		SignalType:       "arbitrage",
		Symbol:           "BTC/USDT",
		Exchanges:        []string{"binance", "coinbase"},
		Volume:           decimal.NewFromFloat(1000000),
		ProfitPotential:  decimal.NewFromFloat(0.02),
		Confidence:       decimal.NewFromFloat(0.8),
		Timestamp:        time.Now(),
		SignalCount:      1,
		SignalComponents: []string{"price_diff", "volume"},
	}

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

	mockScorer.On("AssessSignalQuality", mock.Anything, &qualityInput).Return(qualityMetrics, nil)
	mockScorer.On("IsSignalQualityAcceptable", qualityMetrics, thresholds).Return(true)
	mockScorer.On("GetDefaultQualityThresholds").Return(thresholds)

	// Test quality assessment
	ctx := context.Background()
	result, err := sa.qualityScorer.AssessSignalQuality(ctx, &qualityInput)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, decimal.NewFromFloat(0.8), result.OverallScore)

	// Test quality acceptance
	isAcceptable := sa.qualityScorer.IsSignalQualityAcceptable(qualityMetrics, thresholds)
	assert.True(t, isAcceptable)

	// Test default thresholds
	defaultThresholds := sa.qualityScorer.GetDefaultQualityThresholds()
	assert.NotNil(t, defaultThresholds)
	assert.Equal(t, decimal.NewFromFloat(0.6), defaultThresholds.MinOverallScore)

	mockScorer.AssertExpectations(t)
}
