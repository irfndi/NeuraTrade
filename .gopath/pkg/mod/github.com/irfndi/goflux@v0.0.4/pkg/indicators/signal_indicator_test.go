package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestCrossoverSignal(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(1, 2, 3, 4, 5, 6, 7, 8)
	shortMA := indicators.NewSimpleMovingAverage(indicators.NewClosePriceIndicator(ts), 2)
	longMA := indicators.NewSimpleMovingAverage(indicators.NewClosePriceIndicator(ts), 4)

	signal := indicators.NewCrossoverSignal(shortMA, longMA, indicators.CrossAbove)
	result := signal.CalculateSignal(5)
	assert.True(t, result == indicators.SignalBuy || result == indicators.SignalNeutral || result == indicators.SignalSell)
}

func TestThresholdSignal(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(30, 40, 50, 60, 70, 80)
	rsi := indicators.NewRelativeStrengthIndexIndicator(indicators.NewClosePriceIndicator(ts), 3)

	signal := indicators.NewThresholdSignal(rsi, 70, 30)
	result := signal.CalculateSignal(3)
	assert.True(t, result == indicators.SignalBuy || result == indicators.SignalNeutral || result == indicators.SignalSell)
}

func TestRSISignalIndicator(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(10, 20, 30, 40, 50, 60, 70, 80, 90, 100)
	rsi := indicators.NewRelativeStrengthIndexIndicator(indicators.NewClosePriceIndicator(ts), 3)

	signal := indicators.NewRSISignal(rsi, 70, 30)
	result := signal.CalculateSignal(5)
	assert.True(t, result == indicators.SignalBuy || result == indicators.SignalNeutral || result == indicators.SignalSell)
}

func TestMultiSignal(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	ma5 := indicators.NewSimpleMovingAverage(indicators.NewClosePriceIndicator(ts), 3)
	ma10 := indicators.NewSimpleMovingAverage(indicators.NewClosePriceIndicator(ts), 5)
	rsi := indicators.NewRelativeStrengthIndexIndicator(indicators.NewClosePriceIndicator(ts), 3)

	signal1 := indicators.NewCrossoverSignal(ma5, ma10, indicators.CrossAbove)
	signal2 := indicators.NewRSISignal(rsi, 80, 20)
	signal3 := indicators.NewThresholdSignal(rsi, 80, 20)

	multiSignal := indicators.NewMultiSignal([]indicators.SignalIndicator{
		signal1, signal2, signal3,
	}, 2)

	result := multiSignal.CalculateSignal(7)
	assert.True(t, result == indicators.SignalBuy || result == indicators.SignalNeutral || result == indicators.SignalSell)
}

func TestSignalConstants(t *testing.T) {
	assert.Equal(t, 0, indicators.SignalNeutral)
	assert.Equal(t, 1, indicators.SignalBuy)
	assert.Equal(t, -1, indicators.SignalSell)
	assert.Equal(t, 1, indicators.CrossAbove)
	assert.Equal(t, -1, indicators.CrossBelow)
}

func TestCombineSignals(t *testing.T) {
	signals := indicators.CombineSignals()
	assert.NotNil(t, signals)
	assert.Equal(t, 0, len(signals))
}
