package trading_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/trading"
)

func TestNewFixedFractionalSizer(t *testing.T) {
	sizer := trading.NewFixedFractionalSizer(0.1)
	assert.NotNil(t, sizer)
}

func TestFixedFractionalSizerCalculateSize(t *testing.T) {
	config := trading.PositionSizingConfig{
		Capital:      decimal.New(10000),
		CurrentPrice: decimal.New(100),
		StopLoss:     decimal.New(95),
		RiskPerTrade: decimal.New(0.02),
	}

	sizer := trading.NewFixedFractionalSizer(0.1)
	size := sizer.CalculateSize(config)

	assert.Equal(t, decimal.New(10), size)

	config.Capital = decimal.ZERO
	size = sizer.CalculateSize(config)
	assert.Equal(t, decimal.ZERO, size)
}

func TestNewFixedAmountSizer(t *testing.T) {
	sizer := trading.NewFixedAmountSizer(100)
	assert.NotNil(t, sizer)
}

func TestFixedAmountSizerCalculateSize(t *testing.T) {
	config := trading.PositionSizingConfig{
		Capital:      decimal.New(10000),
		CurrentPrice: decimal.New(100),
		StopLoss:     decimal.New(95),
		RiskPerTrade: decimal.New(0.02),
	}

	sizer := trading.NewFixedAmountSizer(50)
	size := sizer.CalculateSize(config)

	assert.Equal(t, decimal.New(50), size)
}

func TestNewKellyCriterionSizer(t *testing.T) {
	sizer := trading.NewKellyCriterionSizer()
	assert.NotNil(t, sizer)
}

func TestKellyCriterionSizerCalculateSize(t *testing.T) {
	config := trading.PositionSizingConfig{
		Capital:      decimal.New(10000),
		CurrentPrice: decimal.New(100),
		WinRate:      decimal.New(0.6),
		AvgWin:       decimal.New(200),
		AvgLoss:      decimal.New(100),
	}

	sizer := trading.NewKellyCriterionSizer()
	size := sizer.CalculateSize(config)

	assert.Greater(t, size.Float(), 0.0)

	config.WinRate = decimal.ZERO
	size = sizer.CalculateSize(config)
	assert.Equal(t, decimal.ZERO, size)

	config.WinRate = decimal.New(0.6)
	config.AvgLoss = decimal.ZERO
	size = sizer.CalculateSize(config)
	assert.Equal(t, decimal.ZERO, size)
}

func TestNewVolatilityBasedSizer(t *testing.T) {
	sizer := trading.NewVolatilityBasedSizer(2.0)
	assert.NotNil(t, sizer)
}

func TestVolatilityBasedSizerCalculateSize(t *testing.T) {
	config := trading.PositionSizingConfig{
		Capital:      decimal.New(10000),
		CurrentPrice: decimal.New(100),
		StopLoss:     decimal.New(95),
		ATR:          decimal.New(2),
		Volatility:   decimal.New(0.02),
	}

	sizer := trading.NewVolatilityBasedSizer(2.0)
	size := sizer.CalculateSize(config)

	assert.Greater(t, size.Float(), 0.0)

	config.Volatility = decimal.ZERO
	size = sizer.CalculateSize(config)
	assert.Equal(t, decimal.ZERO, size)

	config.Volatility = decimal.New(0.02)
	config.StopLoss = decimal.New(98) // Set a proper stop loss
	size = sizer.CalculateSize(config)
	// With proper stop loss, should calculate a size
	assert.Greater(t, size.Float(), 0.0)

	config.CurrentPrice = decimal.ZERO
	size = sizer.CalculateSize(config)
	assert.Equal(t, decimal.ZERO, size)
}

func TestNewRiskBasedSizer(t *testing.T) {
	sizer := trading.NewRiskBasedSizer()
	assert.NotNil(t, sizer)
}

func TestRiskBasedSizerCalculateSize(t *testing.T) {
	config := trading.PositionSizingConfig{
		Capital:      decimal.New(10000),
		CurrentPrice: decimal.New(100),
		StopLoss:     decimal.New(95),
		RiskPerTrade: decimal.New(0.02),
	}

	sizer := trading.NewRiskBasedSizer()
	size := sizer.CalculateSize(config)

	assert.Greater(t, size.Float(), 0.0)

	config.RiskPerTrade = decimal.ZERO
	size = sizer.CalculateSize(config)
	assert.Equal(t, decimal.ZERO, size)

	config.RiskPerTrade = decimal.New(0.02)
	config.StopLoss = decimal.ZERO
	size = sizer.CalculateSize(config)
	assert.Equal(t, decimal.ZERO, size)

	config.StopLoss = decimal.New(105)
	size = sizer.CalculateSize(config)
	assert.Equal(t, decimal.ZERO, size)
}

func TestNewCanonicalSizer(t *testing.T) {
	sizer := trading.NewCanonicalSizer()
	assert.NotNil(t, sizer)
}

func TestCanonicalSizerCalculateSize(t *testing.T) {
	t.Run("uses volatility-based sizer when ATR and Volatility are set", func(t *testing.T) {
		config := trading.PositionSizingConfig{
			Capital:      decimal.New(10000),
			CurrentPrice: decimal.New(100),
			ATR:          decimal.New(2),
			Volatility:   decimal.New(0.02),
		}

		sizer := trading.NewCanonicalSizer()
		size := sizer.CalculateSize(config)

		assert.Greater(t, size.Float(), 0.0)
	})

	t.Run("uses risk-based sizer when RiskPerTrade is set", func(t *testing.T) {
		config := trading.PositionSizingConfig{
			Capital:      decimal.New(10000),
			CurrentPrice: decimal.New(100),
			StopLoss:     decimal.New(95),
			RiskPerTrade: decimal.New(0.02),
		}

		sizer := trading.NewCanonicalSizer()
		size := sizer.CalculateSize(config)

		assert.Greater(t, size.Float(), 0.0)
	})

	t.Run("uses fixed fractional sizer as fallback", func(t *testing.T) {
		config := trading.PositionSizingConfig{
			Capital:      decimal.New(10000),
			CurrentPrice: decimal.New(100),
		}

		sizer := trading.NewCanonicalSizer()
		size := sizer.CalculateSize(config)

		assert.Greater(t, size.Float(), 0.0)
	})
}
