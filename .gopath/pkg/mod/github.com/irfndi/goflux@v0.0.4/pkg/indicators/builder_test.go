package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestIndicatorBuilder(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105)

	indicator := indicators.NewIndicatorBuilder(ts).
		SMA(2).
		EMA(2).
		Build()

	assert.NotNil(t, indicator)
	val := indicator.Calculate(5)
	assert.True(t, val.IsPositive())
}

func TestIndicatorBuilder_RSI(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110)

	indicator := indicators.NewIndicatorBuilder(ts).
		RSI(5).
		Build()

	assert.NotNil(t, indicator)
	val := indicator.Calculate(10)
	assert.True(t, val.IsPositive())
	assert.LessOrEqual(t, val.Float(), 100.0)
}

func TestIndicatorBuilder_MACD(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112)

	indicator := indicators.NewIndicatorBuilder(ts).
		MACD(12, 26).
		Build()

	assert.NotNil(t, indicator)
	val := indicator.Calculate(12)
	// MACD can be positive or negative
	_ = val
}

func TestIndicatorBuilder_Bollinger(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110)

	upperIndicator := indicators.NewIndicatorBuilder(ts).
		BollingerUpper(5, 2.0).
		Build()

	assert.NotNil(t, upperIndicator)
	upperVal := upperIndicator.Calculate(10)
	assert.True(t, upperVal.IsPositive())

	lowerIndicator := indicators.NewIndicatorBuilder(ts).
		BollingerLower(5, 2.0).
		Build()

	assert.NotNil(t, lowerIndicator)
	lowerVal := lowerIndicator.Calculate(10)
	assert.True(t, lowerVal.IsPositive())
	assert.Less(t, lowerVal.Float(), upperVal.Float())
}
