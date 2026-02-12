package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFuturesArbitrageCalculator(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()
	assert.NotNil(t, calc)
}

func TestCalculateFuturesArbitrage(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	tests := []struct {
		name     string
		input    models.FuturesArbitrageCalculationInput
		wantErr  bool
		checkAPY bool
	}{
		{
			name: "valid arbitrage opportunity",
			input: models.FuturesArbitrageCalculationInput{
				Symbol:             "BTC/USDT",
				LongExchange:       "binance",
				ShortExchange:      "okx",
				LongFundingRate:    decimal.NewFromFloat(-0.005),
				ShortFundingRate:   decimal.NewFromFloat(0.01),
				LongMarkPrice:      decimal.NewFromFloat(50000),
				ShortMarkPrice:     decimal.NewFromFloat(50100),
				BaseAmount:         decimal.NewFromFloat(1.0),
				FundingInterval:    8,
				AvailableCapital:   decimal.NewFromFloat(10000),
				UserRiskTolerance:  "medium",
				MaxLeverageAllowed: decimal.NewFromFloat(10),
			},
			wantErr:  false,
			checkAPY: true,
		},
		{
			name:     "profitable arbitrage",
			checkAPY: true,
			input: models.FuturesArbitrageCalculationInput{
				Symbol:             "BTC/USDT",
				LongExchange:       "binance",
				ShortExchange:      "okx",
				LongFundingRate:    decimal.NewFromFloat(0.0001),
				ShortFundingRate:   decimal.NewFromFloat(0.0002),
				LongMarkPrice:      decimal.NewFromFloat(50000),
				ShortMarkPrice:     decimal.NewFromFloat(50010),
				BaseAmount:         decimal.NewFromFloat(1.0),
				AvailableCapital:   decimal.NewFromFloat(10000),
				UserRiskTolerance:  "medium",
				MaxLeverageAllowed: decimal.NewFromFloat(10),
				FundingInterval:    8,
			},
		},
		{
			name:     "high funding rate difference",
			checkAPY: true,
			input: models.FuturesArbitrageCalculationInput{
				Symbol:             "ETH/USDT",
				LongExchange:       "bybit",
				ShortExchange:      "binance",
				LongFundingRate:    decimal.NewFromFloat(-0.0005),
				ShortFundingRate:   decimal.NewFromFloat(0.001),
				LongMarkPrice:      decimal.NewFromFloat(3000),
				ShortMarkPrice:     decimal.NewFromFloat(3005),
				BaseAmount:         decimal.NewFromFloat(2.0),
				AvailableCapital:   decimal.NewFromFloat(20000),
				UserRiskTolerance:  "high",
				MaxLeverageAllowed: decimal.NewFromFloat(20),
				FundingInterval:    8,
			},
		},
		{
			name: "zero funding rates",
			input: models.FuturesArbitrageCalculationInput{
				Symbol:             "ETH/USDT",
				LongExchange:       "binance",
				ShortExchange:      "okx",
				LongFundingRate:    decimal.Zero,
				ShortFundingRate:   decimal.Zero,
				LongMarkPrice:      decimal.NewFromFloat(3000),
				ShortMarkPrice:     decimal.NewFromFloat(3000),
				BaseAmount:         decimal.NewFromFloat(1.0),
				FundingInterval:    8,
				AvailableCapital:   decimal.NewFromFloat(5000),
				UserRiskTolerance:  "low",
				MaxLeverageAllowed: decimal.NewFromFloat(5),
			},
			wantErr:  false,
			checkAPY: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opportunity, err := calc.CalculateFuturesArbitrage(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, opportunity)
			assert.Equal(t, tt.input.Symbol, opportunity.Symbol)
			assert.Equal(t, tt.input.LongExchange, opportunity.LongExchange)
			assert.Equal(t, tt.input.ShortExchange, opportunity.ShortExchange)

			if tt.checkAPY {
				assert.True(t, opportunity.APY.GreaterThan(decimal.Zero), "APY should be positive for profitable arbitrage")
			}

			// Verify calculated fields are not zero (except for zero funding rates case)
			if !tt.input.LongFundingRate.IsZero() || !tt.input.ShortFundingRate.IsZero() {
				assert.False(t, opportunity.NetFundingRate.IsZero())
				assert.False(t, opportunity.HourlyRate.IsZero())
				assert.False(t, opportunity.DailyRate.IsZero())
			} else {
				// For zero funding rates, these should be zero
				assert.True(t, opportunity.NetFundingRate.IsZero())
				assert.True(t, opportunity.HourlyRate.IsZero())
				assert.True(t, opportunity.DailyRate.IsZero())
			}
		})
	}
}

func TestCalculateRiskScore(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	tests := []struct {
		name     string
		input    models.FuturesArbitrageCalculationInput
		expected decimal.Decimal
	}{
		{
			name: "low risk scenario",
			input: models.FuturesArbitrageCalculationInput{
				Symbol:             "BTC/USDT",
				LongExchange:       "binance",
				ShortExchange:      "okx",
				LongFundingRate:    decimal.NewFromFloat(0.01),
				ShortFundingRate:   decimal.NewFromFloat(-0.005),
				LongMarkPrice:      decimal.NewFromFloat(50000),
				ShortMarkPrice:     decimal.NewFromFloat(50000),
				BaseAmount:         decimal.NewFromFloat(1.0),
				AvailableCapital:   decimal.NewFromFloat(100000),
				UserRiskTolerance:  "low",
				MaxLeverageAllowed: decimal.NewFromFloat(5),
				FundingInterval:    8,
			},
			expected: decimal.NewFromFloat(25), // Expected low risk score
		},
		{
			name: "high risk scenario",
			input: models.FuturesArbitrageCalculationInput{
				Symbol:             "DOGE/USDT",
				LongExchange:       "binance",
				ShortExchange:      "okx",
				LongFundingRate:    decimal.NewFromFloat(0.1),
				ShortFundingRate:   decimal.NewFromFloat(-0.05),
				LongMarkPrice:      decimal.NewFromFloat(0.1),
				ShortMarkPrice:     decimal.NewFromFloat(0.12),
				BaseAmount:         decimal.NewFromFloat(10000),
				AvailableCapital:   decimal.NewFromFloat(1000),
				UserRiskTolerance:  "high",
				MaxLeverageAllowed: decimal.NewFromFloat(20),
				FundingInterval:    8,
			},
			expected: decimal.NewFromFloat(75), // Expected high risk score
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			riskScore := calc.CalculateRiskScore(tt.input)
			assert.True(t, riskScore.GreaterThanOrEqual(decimal.Zero))
			assert.True(t, riskScore.LessThanOrEqual(decimal.NewFromFloat(100)))
			// Risk score should be within reasonable bounds
		})
	}
}

func TestCalculatePositionSizing(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	input := models.FuturesArbitrageCalculationInput{
		Symbol:             "BTC/USDT",
		LongExchange:       "binance",
		ShortExchange:      "okx",
		LongFundingRate:    decimal.NewFromFloat(0.01),
		ShortFundingRate:   decimal.NewFromFloat(-0.005),
		LongMarkPrice:      decimal.NewFromFloat(50000),
		ShortMarkPrice:     decimal.NewFromFloat(50000),
		BaseAmount:         decimal.NewFromFloat(1.0),
		AvailableCapital:   decimal.NewFromFloat(10000),
		UserRiskTolerance:  "medium",
		MaxLeverageAllowed: decimal.NewFromFloat(10),
		FundingInterval:    8,
	}

	riskScore := decimal.NewFromFloat(50)
	positionSizing := calc.CalculatePositionSizing(input, riskScore)

	assert.NotNil(t, positionSizing)
	assert.True(t, positionSizing.KellyPercentage.GreaterThanOrEqual(decimal.Zero))
	assert.True(t, positionSizing.KellyPositionSize.GreaterThan(decimal.Zero))
	assert.True(t, positionSizing.ConservativeSize.GreaterThan(decimal.Zero))
	assert.True(t, positionSizing.ModerateSize.GreaterThan(decimal.Zero))
	assert.True(t, positionSizing.AggressiveSize.GreaterThan(decimal.Zero))

	// Conservative should be smaller than moderate, moderate smaller than aggressive
	assert.True(t, positionSizing.ConservativeSize.LessThanOrEqual(positionSizing.ModerateSize))
	assert.True(t, positionSizing.ModerateSize.LessThanOrEqual(positionSizing.AggressiveSize))

	// Leverage values should be reasonable
	assert.True(t, positionSizing.MinLeverage.GreaterThan(decimal.Zero))
	assert.True(t, positionSizing.OptimalLeverage.GreaterThanOrEqual(positionSizing.MinLeverage))
	assert.True(t, positionSizing.MaxSafeLeverage.GreaterThanOrEqual(positionSizing.OptimalLeverage))
}

func TestCalculateRiskMetrics(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	input := models.FuturesArbitrageCalculationInput{
		Symbol:             "BTC/USDT",
		LongExchange:       "binance",
		ShortExchange:      "okx",
		LongFundingRate:    decimal.NewFromFloat(0.01),
		ShortFundingRate:   decimal.NewFromFloat(-0.005),
		LongMarkPrice:      decimal.NewFromFloat(50000),
		ShortMarkPrice:     decimal.NewFromFloat(50000),
		BaseAmount:         decimal.NewFromFloat(1.0),
		AvailableCapital:   decimal.NewFromFloat(10000),
		UserRiskTolerance:  "medium",
		MaxLeverageAllowed: decimal.NewFromFloat(10),
		FundingInterval:    8,
	}

	// Create some mock historical data
	historicalData := []models.FundingRateHistoryPoint{
		{
			Timestamp:   time.Now().Add(-24 * time.Hour),
			FundingRate: decimal.NewFromFloat(0.01),
			MarkPrice:   decimal.NewFromFloat(50000),
		},
		{
			Timestamp:   time.Now().Add(-16 * time.Hour),
			FundingRate: decimal.NewFromFloat(0.008),
			MarkPrice:   decimal.NewFromFloat(50100),
		},
		{
			Timestamp:   time.Now().Add(-8 * time.Hour),
			FundingRate: decimal.NewFromFloat(0.012),
			MarkPrice:   decimal.NewFromFloat(49900),
		},
	}

	riskMetrics := calc.CalculateRiskMetrics(input, historicalData)

	assert.NotNil(t, riskMetrics)
	// Verify risk metrics
	assert.True(t, riskMetrics.PriceVolatility.GreaterThan(decimal.Zero))
	assert.True(t, riskMetrics.FundingRateVolatility.GreaterThan(decimal.Zero))
	assert.True(t, riskMetrics.BidAskSpread.GreaterThanOrEqual(decimal.Zero))
	assert.True(t, riskMetrics.MarketDepth.GreaterThanOrEqual(decimal.Zero))

	// Verify overall risk score is calculated
	assert.True(t, riskMetrics.OverallRiskScore.GreaterThanOrEqual(decimal.Zero))
	assert.True(t, riskMetrics.OverallRiskScore.LessThanOrEqual(decimal.NewFromFloat(100)))
}

func TestCalculateAPY(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	hourlyRate := decimal.NewFromFloat(0.001) // 0.1% per hour

	apy := calc.CalculateAPY(hourlyRate)

	t.Logf("Hourly rate: %s, APY: %s", hourlyRate.String(), apy.String())
	assert.True(t, apy.GreaterThan(decimal.Zero), "APY should be positive, got: %s", apy.String())
	// APY should be positive for positive funding rate
	// 0.1% per hour compounded annually = very high APY, so use realistic upper bound
	assert.True(t, apy.LessThan(decimal.NewFromFloat(1000000))) // Reasonable upper bound for compound interest
}

func TestCalculateEstimatedProfits(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	baseAmount := decimal.NewFromFloat(1000.0)
	hourlyRate := decimal.NewFromFloat(0.001) // 0.1% per hour

	profit8h := calc.calculatePeriodProfit(baseAmount, hourlyRate, 8)
	profitDaily := calc.calculatePeriodProfit(baseAmount, hourlyRate, 24)
	profitWeekly := calc.calculatePeriodProfit(baseAmount, hourlyRate, 168)  // 24*7
	profitMonthly := calc.calculatePeriodProfit(baseAmount, hourlyRate, 720) // 24*30

	assert.True(t, profit8h.GreaterThan(decimal.Zero))
	assert.True(t, profitDaily.GreaterThan(profit8h))
	assert.True(t, profitWeekly.GreaterThan(profitDaily))
	assert.True(t, profitMonthly.GreaterThan(profitWeekly))
}

func TestCalculateSymbolArbitrage_GeneratesValidUUID(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	// Create market data with price differences to trigger arbitrage opportunity detection
	exchangeData := map[string]models.MarketData{
		"binance": {
			ExchangeID:    1,
			TradingPairID: 1,
			LastPrice:     decimal.NewFromFloat(50000),
			TradingPair:   &models.TradingPair{ID: 1, Symbol: "BTC/USDT"},
		},
		"okx": {
			ExchangeID:    2,
			TradingPairID: 1,
			LastPrice:     decimal.NewFromFloat(50100), // Higher price - significant difference
			TradingPair:   &models.TradingPair{ID: 1, Symbol: "BTC/USDT"},
		},
	}

	opportunities := calc.calculateSymbolArbitrage("BTC/USDT", exchangeData)

	// Should have found an opportunity due to price difference
	require.NotEmpty(t, opportunities, "Should find arbitrage opportunity with price difference")

	for _, opp := range opportunities {
		// Verify that the ID is a valid UUID
		assert.NotEmpty(t, opp.ID, "Opportunity ID should not be empty")

		// Parse the ID as UUID to verify format
		parsedUUID, err := uuid.Parse(opp.ID)
		require.NoError(t, err, "ID should be a valid UUID format, got: %s", opp.ID)
		assert.Equal(t, opp.ID, parsedUUID.String(), "Parsed UUID should match original")
	}
}

func TestCalculateArbitrageOpportunities_GeneratesValidUUIDs(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()
	ctx := context.Background()

	// Create market data across exchanges with price differences
	marketData := map[string][]models.MarketData{
		"binance": {
			{
				ExchangeID:    1,
				TradingPairID: 1,
				LastPrice:     decimal.NewFromFloat(50000),
				TradingPair:   &models.TradingPair{ID: 1, Symbol: "BTC/USDT"},
			},
		},
		"okx": {
			{
				ExchangeID:    2,
				TradingPairID: 1,
				LastPrice:     decimal.NewFromFloat(50100), // Higher price
				TradingPair:   &models.TradingPair{ID: 1, Symbol: "BTC/USDT"},
			},
		},
	}

	opportunities, err := calc.CalculateArbitrageOpportunities(ctx, marketData)
	require.NoError(t, err)
	require.NotEmpty(t, opportunities, "Should find arbitrage opportunities with price differences")

	for _, opp := range opportunities {
		// Verify that the ID is a valid UUID
		assert.NotEmpty(t, opp.ID, "Opportunity ID should not be empty")

		parsedUUID, err := uuid.Parse(opp.ID)
		require.NoError(t, err, "ID should be a valid UUID format, got: %s", opp.ID)
		assert.Equal(t, opp.ID, parsedUUID.String(), "Parsed UUID should match original")

		// Also verify other fields are set correctly
		assert.NotEmpty(t, opp.BuyExchange, "BuyExchange should be set")
		assert.NotEmpty(t, opp.SellExchange, "SellExchange should be set")
	}
}

func TestCalculateSymbolArbitrage_NoProfitOpportunity(t *testing.T) {
	calc := NewFuturesArbitrageCalculator()

	// Create market data with minimal price difference (below 0.1% threshold)
	exchangeData := map[string]models.MarketData{
		"binance": {
			ExchangeID:    1,
			TradingPairID: 1,
			LastPrice:     decimal.NewFromFloat(50000),
			TradingPair:   &models.TradingPair{ID: 1, Symbol: "BTC/USDT"},
		},
		"okx": {
			ExchangeID:    2,
			TradingPairID: 1,
			LastPrice:     decimal.NewFromFloat(50000), // Same price - no opportunity
			TradingPair:   &models.TradingPair{ID: 1, Symbol: "BTC/USDT"},
		},
	}

	opportunities := calc.calculateSymbolArbitrage("BTC/USDT", exchangeData)

	// Should not find any opportunities with same prices
	assert.Empty(t, opportunities, "Should not find arbitrage opportunity with same prices")
}
