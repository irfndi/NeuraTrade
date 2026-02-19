package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestT3(t *testing.T) {
	tsValues := make([]float64, 250)
	for i := range tsValues {
		tsValues[i] = 100
	}
	ts := testutils.MockTimeSeriesFl(tsValues...)
	closeInd := indicators.NewClosePriceIndicator(ts)
	t3 := indicators.NewT3Indicator(closeInd, 5, 0.7)

	val := t3.Calculate(249)
	assert.NotNil(t, val)
	assert.True(t, val.Sub(decimal.New(100)).Abs().LT(decimal.New(0.0001)))
}

func TestALMA(t *testing.T) {
	tsValues := make([]float64, 50)
	for i := range tsValues {
		tsValues[i] = 100
	}
	ts := testutils.MockTimeSeriesFl(tsValues...)
	closeInd := indicators.NewClosePriceIndicator(ts)
	alma := indicators.NewALMAIndicator(closeInd, 5, 0.85, 6.0)

	val := alma.Calculate(49)
	assert.NotNil(t, val)
	assert.True(t, val.Sub(decimal.New(100)).Abs().LT(decimal.New(0.0001)))
}

func TestVIDYA(t *testing.T) {
	tsValues := make([]float64, 50)
	for i := range tsValues {
		tsValues[i] = 100
	}
	ts := testutils.MockTimeSeriesFl(tsValues...)
	closeInd := indicators.NewClosePriceIndicator(ts)
	vidya := indicators.NewVIDYAIndicator(closeInd, 5)

	val := vidya.Calculate(49)
	assert.NotNil(t, val)
	assert.True(t, val.EQ(decimal.New(100)))
}

func TestMAMA(t *testing.T) {
	tsValues := make([]float64, 50)
	for i := range tsValues {
		tsValues[i] = 100
	}
	ts := testutils.MockTimeSeriesFl(tsValues...)
	closeInd := indicators.NewClosePriceIndicator(ts)
	mama := indicators.NewMAMAIndicator(closeInd, 0.5, 0.05)

	val := mama.Calculate(49)
	assert.NotNil(t, val)
	assert.True(t, val.EQ(decimal.New(100)))
}

func TestFAMA(t *testing.T) {
	tsValues := make([]float64, 50)
	for i := range tsValues {
		tsValues[i] = 100
	}
	ts := testutils.MockTimeSeriesFl(tsValues...)
	closeInd := indicators.NewClosePriceIndicator(ts)
	fama := indicators.NewFAMAIndicator(closeInd, 0.5, 0.05)

	famaVal := fama.Calculate(49)
	assert.True(t, famaVal.EQ(decimal.New(100)))
}

func TestFAMAFollowsMAMA(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12)
	closeInd := indicators.NewClosePriceIndicator(ts)
	mama := indicators.NewMAMAIndicator(closeInd, 0.5, 0.05)
	fama := indicators.NewFAMAIndicator(closeInd, 0.5, 0.05)

	mamaVal := mama.Calculate(11)
	famaVal := fama.Calculate(11)
	assert.True(t, famaVal.LT(mamaVal))
}
