package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
)

func TestNewADLineIndicator(t *testing.T) {
	ts := series.NewTimeSeries()
	indicator := indicators.NewADLineIndicator(ts)

	assert.NotNil(t, indicator)
}

func TestADLineIndicator_Calculate(t *testing.T) {
	ts := series.NewTimeSeries()

	// Add some candles with different patterns
	candle1 := &series.Candle{
		OpenPrice:  decimal.New(100),
		ClosePrice: decimal.New(105),
		MaxPrice:   decimal.New(110),
		MinPrice:   decimal.New(95),
		Volume:     decimal.New(1000),
	}
	ts.AddCandle(candle1)

	candle2 := &series.Candle{
		OpenPrice:  decimal.New(105),
		ClosePrice: decimal.New(110),
		MaxPrice:   decimal.New(115),
		MinPrice:   decimal.New(100),
		Volume:     decimal.New(1500),
	}
	ts.AddCandle(candle2)

	candle3 := &series.Candle{
		OpenPrice:  decimal.New(110),
		ClosePrice: decimal.New(108),
		MaxPrice:   decimal.New(112),
		MinPrice:   decimal.New(105),
		Volume:     decimal.New(800),
	}
	ts.AddCandle(candle3)

	indicator := indicators.NewADLineIndicator(ts)

	// Test first candle
	val := indicator.Calculate(0)
	assert.True(t, val.IsPositive())

	// Test second candle (should be greater than first since price went up)
	val2 := indicator.Calculate(1)
	assert.True(t, val2.IsPositive())
	assert.True(t, val2.GT(val))

	// Test third candle (price went down slightly)
	val3 := indicator.Calculate(2)
	assert.True(t, val3.IsPositive())
	// May be less than val2 since price decreased
}

func TestADLineIndicator_OutOfBounds(t *testing.T) {
	ts := series.NewTimeSeries()

	candle := &series.Candle{
		OpenPrice:  decimal.New(100),
		ClosePrice: decimal.New(105),
		MaxPrice:   decimal.New(110),
		MinPrice:   decimal.New(95),
		Volume:     decimal.New(1000),
	}
	ts.AddCandle(candle)

	indicator := indicators.NewADLineIndicator(ts)

	// Test negative index
	val := indicator.Calculate(-1)
	assert.Equal(t, decimal.New(0), val)

	// Test out of bounds index
	val = indicator.Calculate(10)
	assert.Equal(t, decimal.New(0), val)
}
