package services

import (
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPYOverflowProtection tests that extreme funding rates don't cause overflow
func TestAPYOverflowProtection(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	tests := []struct {
		name        string
		hourlyRate  decimal.Decimal
		expectedMax decimal.Decimal
		expectedMin decimal.Decimal
		description string
	}{
		{
			name:        "normal positive rate",
			hourlyRate:  decimal.NewFromFloat(0.0001), // 0.01% per hour
			expectedMax: decimal.NewFromInt(1000),
			expectedMin: decimal.NewFromInt(0),
			description: "Normal rates should calculate without capping",
		},
		{
			name:        "high positive rate",
			hourlyRate:  decimal.NewFromFloat(0.001), // 0.1% per hour
			expectedMax: decimal.NewFromInt(1000),
			expectedMin: decimal.NewFromInt(0),
			description: "High but reasonable rates",
		},
		{
			name:        "extreme positive rate - should be capped",
			hourlyRate:  decimal.NewFromFloat(0.05), // 5% per hour - extreme
			expectedMax: decimal.NewFromInt(1000),
			expectedMin: decimal.NewFromInt(100),
			description: "Extreme rates should be capped at 1000%",
		},
		{
			name:        "overflow-causing rate - should be capped",
			hourlyRate:  decimal.NewFromFloat(0.5), // 50% per hour - would overflow
			expectedMax: decimal.NewFromInt(1000),
			expectedMin: decimal.NewFromInt(500),
			description: "Overflow-causing rates should be safely capped",
		},
		{
			name:        "negative rate",
			hourlyRate:  decimal.NewFromFloat(-0.001),
			expectedMax: decimal.NewFromInt(0),
			expectedMin: decimal.NewFromInt(-100),
			description: "Negative rates should return negative APY",
		},
		{
			name:        "extreme negative rate - should be capped",
			hourlyRate:  decimal.NewFromFloat(-0.5),
			expectedMax: decimal.NewFromInt(-100),
			expectedMin: decimal.NewFromInt(-100),
			description: "Extreme negative rates should be capped at -100%",
		},
		{
			name:        "zero rate",
			hourlyRate:  decimal.Zero,
			expectedMax: decimal.NewFromFloat(0.001),
			expectedMin: decimal.NewFromFloat(-0.001),
			description: "Zero rate should return ~0%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apy := calc.CalculateAPY(tt.hourlyRate)

			t.Logf("%s: hourly rate=%s, APY=%s%%",
				tt.description, tt.hourlyRate.String(), apy.String())

			// APY should be within bounds
			assert.True(t, apy.LessThanOrEqual(tt.expectedMax),
				"APY %s should be <= %s", apy.String(), tt.expectedMax.String())
			assert.True(t, apy.GreaterThanOrEqual(tt.expectedMin),
				"APY %s should be >= %s", apy.String(), tt.expectedMin.String())

			// APY should never be NaN or Inf (check via string representation)
			apyStr := apy.String()
			assert.NotContains(t, apyStr, "NaN")
			assert.NotContains(t, apyStr, "Inf")
		})
	}
}

// TestNextFundingTimeCalculation tests the funding time calculation via CalculateFuturesArbitrage
func TestNextFundingTimeCalculation(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	// Test via the public CalculateFuturesArbitrage method which uses calculateNextFundingTime internally
	input := models.FuturesArbitrageCalculationInput{
		Symbol:             "BTC/USDT",
		LongExchange:       "binance",
		ShortExchange:      "okx",
		LongFundingRate:    decimal.NewFromFloat(0.01),
		ShortFundingRate:   decimal.NewFromFloat(-0.005),
		LongMarkPrice:      decimal.NewFromInt(50000),
		ShortMarkPrice:     decimal.NewFromInt(50000),
		BaseAmount:         decimal.NewFromFloat(1.0),
		FundingInterval:    8,
		AvailableCapital:   decimal.NewFromFloat(10000),
		UserRiskTolerance:  "medium",
		MaxLeverageAllowed: decimal.NewFromFloat(10),
	}

	opportunity, err := calc.CalculateFuturesArbitrage(input)
	require.NoError(t, err)
	require.NotNil(t, opportunity)

	// Verify that next funding time is calculated and in the future
	assert.False(t, opportunity.NextFundingTime.IsZero(), "next funding time should be set")
	assert.True(t, opportunity.NextFundingTime.After(time.Now()) || opportunity.NextFundingTime.Equal(time.Now()),
		"next funding time should be in the future or now")

	// Verify time to next funding is reasonable (0 to 8 hours = 0 to 480 minutes)
	assert.GreaterOrEqual(t, opportunity.TimeToNextFunding, 0)
	assert.LessOrEqual(t, opportunity.TimeToNextFunding, 480)

	t.Logf("Next funding: %s, Time to next: %d min",
		opportunity.NextFundingTime.Format("2006-01-02 15:04:05"),
		opportunity.TimeToNextFunding)
}

// TestSlippageEstimation tests slippage calculation with order book data
func TestSlippageEstimation(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	tests := []struct {
		name             string
		positionSize     decimal.Decimal
		orderBookMetrics *OrderBookMetricsInput
		expectedMinSlip  decimal.Decimal
		expectedMaxSlip  decimal.Decimal
	}{
		{
			name:             "small position - no order book data",
			positionSize:     decimal.NewFromInt(10000),
			orderBookMetrics: nil, // No order book data
			expectedMinSlip:  decimal.NewFromFloat(0.04),
			expectedMaxSlip:  decimal.NewFromFloat(0.06),
		},
		{
			name:             "medium position - no order book data",
			positionSize:     decimal.NewFromInt(75000),
			orderBookMetrics: nil,
			expectedMinSlip:  decimal.NewFromFloat(0.09),
			expectedMaxSlip:  decimal.NewFromFloat(0.11),
		},
		{
			name:             "large position - no order book data",
			positionSize:     decimal.NewFromInt(150000),
			orderBookMetrics: nil,
			expectedMinSlip:  decimal.NewFromFloat(0.19),
			expectedMaxSlip:  decimal.NewFromFloat(0.21),
		},
		{
			name:         "with order book data",
			positionSize: decimal.NewFromInt(10000),
			orderBookMetrics: &OrderBookMetricsInput{
				LongExchangeMetrics: &models.OrderBookMetrics{
					SlippageEstimates: map[string]models.SlippageEstimate{
						"10000": {
							PositionSize: decimal.NewFromInt(10000),
							BuySlippage:  decimal.NewFromFloat(0.02),
							SellSlippage: decimal.NewFromFloat(0.02),
							IsFillable:   true,
						},
					},
				},
				ShortExchangeMetrics: &models.OrderBookMetrics{
					SlippageEstimates: map[string]models.SlippageEstimate{
						"10000": {
							PositionSize: decimal.NewFromInt(10000),
							BuySlippage:  decimal.NewFromFloat(0.03),
							SellSlippage: decimal.NewFromFloat(0.03),
							IsFillable:   true,
						},
					},
				},
			},
			expectedMinSlip: decimal.NewFromFloat(0.04), // 0.02 + 0.03 (buy on long, sell on short)
			expectedMaxSlip: decimal.NewFromFloat(0.06),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slippage := calc.EstimateExecutionSlippage(tt.positionSize, tt.orderBookMetrics)

			t.Logf("Position: $%s, Slippage: %s%%",
				tt.positionSize.String(), slippage.String())

			assert.True(t, slippage.GreaterThanOrEqual(tt.expectedMinSlip),
				"slippage should be >= %s%%, got %s%%",
				tt.expectedMinSlip.String(), slippage.String())
			assert.True(t, slippage.LessThanOrEqual(tt.expectedMaxSlip),
				"slippage should be <= %s%%, got %s%%",
				tt.expectedMaxSlip.String(), slippage.String())
		})
	}
}

// TestLiquidityScoreWithOrderBook tests liquidity score calculation
func TestLiquidityScoreWithOrderBook(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	tests := []struct {
		name             string
		orderBookMetrics *OrderBookMetricsInput
		baseAmount       decimal.Decimal
		expectedMin      decimal.Decimal
		expectedMax      decimal.Decimal
	}{
		{
			name: "excellent liquidity on both exchanges",
			orderBookMetrics: &OrderBookMetricsInput{
				LongExchangeMetrics: &models.OrderBookMetrics{
					BidAskSpread:   decimal.NewFromFloat(0.01),
					BidDepth1Pct:   decimal.NewFromInt(10000000),
					AskDepth1Pct:   decimal.NewFromInt(10000000),
					LiquidityScore: decimal.NewFromInt(95),
				},
				ShortExchangeMetrics: &models.OrderBookMetrics{
					BidAskSpread:   decimal.NewFromFloat(0.01),
					BidDepth1Pct:   decimal.NewFromInt(10000000),
					AskDepth1Pct:   decimal.NewFromInt(10000000),
					LiquidityScore: decimal.NewFromInt(95),
				},
			},
			baseAmount:  decimal.NewFromInt(10000),
			expectedMin: decimal.NewFromInt(85),
			expectedMax: decimal.NewFromInt(100),
		},
		{
			name: "asymmetric liquidity - wide spread on one side",
			orderBookMetrics: &OrderBookMetricsInput{
				LongExchangeMetrics: &models.OrderBookMetrics{
					BidAskSpread:   decimal.NewFromFloat(0.01),
					BidDepth1Pct:   decimal.NewFromInt(10000000),
					AskDepth1Pct:   decimal.NewFromInt(10000000),
					LiquidityScore: decimal.NewFromInt(95),
				},
				ShortExchangeMetrics: &models.OrderBookMetrics{
					BidAskSpread:   decimal.NewFromFloat(0.1),
					BidDepth1Pct:   decimal.NewFromInt(100000),
					AskDepth1Pct:   decimal.NewFromInt(100000),
					LiquidityScore: decimal.NewFromInt(50),
				},
			},
			baseAmount: decimal.NewFromInt(50000),
			// Calculation: avgSpread=0.055, spreadScore=89, depthScore=100, slippageScore=100
			// Final: 89*0.4 + 100*0.35 + 100*0.25 = 35.6 + 35 + 25 = 95.6
			expectedMin: decimal.NewFromInt(90),
			expectedMax: decimal.NewFromInt(100),
		},
		{
			name:             "nil order book - uses fallback",
			orderBookMetrics: nil,
			baseAmount:       decimal.NewFromInt(10000),
			expectedMin:      decimal.NewFromInt(0),
			expectedMax:      decimal.NewFromInt(100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := models.FuturesArbitrageCalculationInput{
				Symbol:         "BTC/USDT",
				LongExchange:   "binance",
				ShortExchange:  "okx",
				BaseAmount:     tt.baseAmount,
				LongMarkPrice:  decimal.NewFromInt(50000),
				ShortMarkPrice: decimal.NewFromInt(50000),
			}

			liquidityScore := calc.calculateLiquidityScoreWithOrderBook(input, tt.orderBookMetrics)

			t.Logf("Liquidity score: %s (expected: %s-%s)",
				liquidityScore.String(), tt.expectedMin.String(), tt.expectedMax.String())

			assert.True(t, liquidityScore.GreaterThanOrEqual(tt.expectedMin),
				"liquidity score should be >= %s", tt.expectedMin.String())
			assert.True(t, liquidityScore.LessThanOrEqual(tt.expectedMax),
				"liquidity score should be <= %s", tt.expectedMax.String())
		})
	}
}

// TestCalculateFuturesArbitrageWithOrderBook tests the enhanced calculation
func TestCalculateFuturesArbitrageWithOrderBook(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	input := models.FuturesArbitrageCalculationInput{
		Symbol:             "BTC/USDT",
		LongExchange:       "binance",
		ShortExchange:      "okx",
		LongFundingRate:    decimal.NewFromFloat(-0.01),
		ShortFundingRate:   decimal.NewFromFloat(0.02),
		LongMarkPrice:      decimal.NewFromInt(50000),
		ShortMarkPrice:     decimal.NewFromInt(50050),
		BaseAmount:         decimal.NewFromInt(10000),
		FundingInterval:    8,
		AvailableCapital:   decimal.NewFromInt(100000),
		UserRiskTolerance:  "medium",
		MaxLeverageAllowed: decimal.NewFromInt(10),
	}

	orderBookMetrics := &OrderBookMetricsInput{
		LongExchangeMetrics: &models.OrderBookMetrics{
			Exchange:       "binance",
			Symbol:         "BTC/USDT",
			BidAskSpread:   decimal.NewFromFloat(0.02),
			MidPrice:       decimal.NewFromInt(50000),
			BidDepth1Pct:   decimal.NewFromInt(5000000),
			AskDepth1Pct:   decimal.NewFromInt(5000000),
			LiquidityScore: decimal.NewFromInt(90),
		},
		ShortExchangeMetrics: &models.OrderBookMetrics{
			Exchange:       "okx",
			Symbol:         "BTC/USDT",
			BidAskSpread:   decimal.NewFromFloat(0.03),
			MidPrice:       decimal.NewFromInt(50050),
			BidDepth1Pct:   decimal.NewFromInt(3000000),
			AskDepth1Pct:   decimal.NewFromInt(3000000),
			LiquidityScore: decimal.NewFromInt(85),
		},
	}

	opportunity, err := calc.CalculateFuturesArbitrageWithOrderBook(input, orderBookMetrics)

	require.NoError(t, err)
	require.NotNil(t, opportunity)

	// Verify opportunity fields
	assert.Equal(t, input.Symbol, opportunity.Symbol)
	assert.Equal(t, input.LongExchange, opportunity.LongExchange)
	assert.Equal(t, input.ShortExchange, opportunity.ShortExchange)

	// APY should be positive for this configuration
	assert.True(t, opportunity.APY.IsPositive())

	// Liquidity score should be calculated with order book data
	assert.True(t, opportunity.LiquidityScore.IsPositive())
	assert.True(t, opportunity.LiquidityScore.LessThanOrEqual(decimal.NewFromInt(100)))

	// Risk score should account for liquidity
	assert.True(t, opportunity.RiskScore.GreaterThanOrEqual(decimal.Zero))
	assert.True(t, opportunity.RiskScore.LessThanOrEqual(decimal.NewFromInt(100)))

	t.Logf("Opportunity: APY=%s%%, RiskScore=%s, LiquidityScore=%s",
		opportunity.APY.String(),
		opportunity.RiskScore.String(),
		opportunity.LiquidityScore.String())
}

// TestFundingRateStats tests the funding rate statistics model
func TestFundingRateStats(t *testing.T) {
	stats := &models.FundingRateStats{
		Symbol:          "BTC/USDT",
		Exchange:        "binance",
		CurrentRate:     decimal.NewFromFloat(0.01),
		AvgRate7d:       decimal.NewFromFloat(0.008),
		AvgRate30d:      decimal.NewFromFloat(0.007),
		StdDev7d:        decimal.NewFromFloat(0.002),
		StdDev30d:       decimal.NewFromFloat(0.003),
		MinRate7d:       decimal.NewFromFloat(0.002),
		MaxRate7d:       decimal.NewFromFloat(0.015),
		TrendDirection:  "increasing",
		TrendStrength:   decimal.NewFromFloat(0.7),
		VolatilityScore: decimal.NewFromFloat(25),
		StabilityScore:  decimal.NewFromFloat(75),
		DataPoints:      84, // 7 days * 3 funding periods/day * 4 (approximate)
		LastUpdated:     time.Now(),
	}

	assert.Equal(t, "BTC/USDT", stats.Symbol)
	assert.True(t, stats.CurrentRate.GreaterThan(stats.AvgRate7d))
	assert.Equal(t, "increasing", stats.TrendDirection)
	assert.True(t, stats.TrendStrength.LessThanOrEqual(decimal.NewFromInt(1)))
	assert.True(t, stats.StabilityScore.Add(stats.VolatilityScore).Equal(decimal.NewFromInt(100)))
}

// TestOrderBookMetricsMethods tests helper methods on OrderBookMetrics
func TestOrderBookMetricsMethods(t *testing.T) {
	metrics := &models.OrderBookMetrics{
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		BestBid:       decimal.NewFromInt(49990),
		BestAsk:       decimal.NewFromInt(50010),
		MidPrice:      decimal.NewFromInt(50000),
		BidAskSpread:  decimal.NewFromFloat(0.04), // 0.04%
		BidDepth1Pct:  decimal.NewFromInt(1000000),
		AskDepth1Pct:  decimal.NewFromInt(800000),
		Imbalance1Pct: decimal.NewFromFloat(0.111), // (1M-0.8M)/(1M+0.8M) = 0.2M/1.8M
		SlippageEstimates: map[string]models.SlippageEstimate{
			"10000": {
				PositionSize: decimal.NewFromInt(10000),
				BuySlippage:  decimal.NewFromFloat(0.01),
				SellSlippage: decimal.NewFromFloat(0.01),
				IsFillable:   true,
			},
		},
		LiquidityScore: decimal.NewFromInt(80),
	}

	// Test CalculateSpread
	spread := metrics.CalculateSpread()
	t.Logf("Calculated spread: %s%%", spread.String())
	assert.True(t, spread.GreaterThan(decimal.Zero))
	assert.True(t, spread.LessThan(decimal.NewFromFloat(1))) // Less than 1%

	// Test IsLiquidEnough
	assert.True(t, metrics.IsLiquidEnough(
		decimal.NewFromInt(10000),
		decimal.NewFromFloat(0.5),
	))

	// Test GetImbalanceSignal
	signal := metrics.GetImbalanceSignal()
	t.Logf("Imbalance signal: %s (imbalance: %s)", signal, metrics.Imbalance1Pct.String())
	// 11.1% imbalance is below 20% threshold, so should be neutral
	assert.Equal(t, "neutral", signal)

	// Test with bullish imbalance
	metrics.Imbalance1Pct = decimal.NewFromFloat(0.25) // 25%
	assert.Equal(t, "bullish", metrics.GetImbalanceSignal())

	// Test with bearish imbalance
	metrics.Imbalance1Pct = decimal.NewFromFloat(-0.25)
	assert.Equal(t, "bearish", metrics.GetImbalanceSignal())
}

// TestExchangeReliabilityMetrics tests the reliability metrics model
func TestExchangeReliabilityMetrics(t *testing.T) {
	lastFailure := time.Now().Add(-1 * time.Hour)

	metrics := &models.ExchangeReliabilityMetrics{
		Exchange:         "binance",
		UptimePercent24h: decimal.NewFromFloat(99.5),
		UptimePercent7d:  decimal.NewFromFloat(99.8),
		AvgLatencyMs:     85,
		FailureCount24h:  5,
		FailureCount7d:   12,
		LastFailure:      &lastFailure,
		RiskScore:        decimal.NewFromFloat(5.5),
		LastUpdated:      time.Now(),
	}

	assert.Equal(t, "binance", metrics.Exchange)
	assert.True(t, metrics.UptimePercent24h.GreaterThan(decimal.NewFromInt(99)))
	assert.True(t, metrics.RiskScore.LessThan(decimal.NewFromInt(10)))
	assert.NotNil(t, metrics.LastFailure)
	assert.True(t, metrics.LastFailure.Before(time.Now()))
}
