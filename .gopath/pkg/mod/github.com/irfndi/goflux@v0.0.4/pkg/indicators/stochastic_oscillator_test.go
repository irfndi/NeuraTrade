package indicators_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

var fastStochValues = []float64{
	100,
	100,
	100.0 * 12.0 / 16.0,
	100.0 * 2.0 / 16.0,
	100.0 * 6.0 / 16.0,
	100.0 * 2.0 / 16.0,
	100.0 * 3.0 / 15.0,
	100.0 * 2.0 / 16.0,
	100.0 * 4.0 / 13.0,
	100.0 * 11.0 / 17.0,
	100.0 * 24.0 / 49.0,
}

func TestFastStochasticIndicator(t *testing.T) {
	ts := testutils.MockTimeSeriesOCHL(
		[]float64{10, 12, 12, 8},
		[]float64{11, 14, 14, 9},
		[]float64{10, 20, 24, 10},
		[]float64{9, 10, 11, 9},
		[]float64{11, 14, 14, 9},
		[]float64{9, 10, 11, 9},
		[]float64{10, 12, 12, 10},
		[]float64{9, 10, 11, 8},
		[]float64{6, 5, 8, 1},
		[]float64{15, 12, 18, 9},
		[]float64{35, 25, 50, 20},
	)

	window := 6

	k := indicators.NewFastStochasticIndicator(ts, window)

	testutils.DecimalEquals(t, fastStochValues[0], k.Calculate(0))
	testutils.DecimalEquals(t, fastStochValues[1], k.Calculate(1))
	testutils.DecimalEquals(t, fastStochValues[2], k.Calculate(2))
	testutils.DecimalEquals(t, fastStochValues[3], k.Calculate(3))
	testutils.DecimalEquals(t, fastStochValues[4], k.Calculate(4))
	testutils.DecimalEquals(t, fastStochValues[5], k.Calculate(5))
	testutils.DecimalEquals(t, fastStochValues[6], k.Calculate(6))
	testutils.DecimalEquals(t, fastStochValues[7], k.Calculate(7))
	testutils.DecimalEquals(t, fastStochValues[8], k.Calculate(8))
	testutils.DecimalEquals(t, fastStochValues[9], k.Calculate(9))
	testutils.DecimalEquals(t, fastStochValues[10], k.Calculate(10))
}

func TestSlowStochasticIndicator(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(fastStochValues...)

	window := 3

	d := indicators.NewSlowStochasticIndicator(indicators.NewClosePriceIndicator(ts), window)

	testutils.DecimalEquals(t, 0, d.Calculate(0))
	testutils.DecimalEquals(t, 0, d.Calculate(1))
	testutils.DecimalEquals(t, 100.0*(12.0/16.0+1+1)/3.0, d.Calculate(2))
	testutils.DecimalEquals(t, 100.0*(2.0/16.0+12.0/16.0+1)/3.0, d.Calculate(3))
	testutils.DecimalEquals(t, 100.0*(6.0/16.0+2.0/16.0+12.0/16.0)/3.0, d.Calculate(4))
	testutils.DecimalEquals(t, 100.0*(2.0/16.0+6.0/16.0+2.0/16.0)/3.0, d.Calculate(5))
	testutils.DecimalEquals(t, 100.0*(3.0/15.0+2.0/16.0+6.0/16.0)/3.0, d.Calculate(6))
	testutils.DecimalEquals(t, 100.0*(2.0/16.0+3.0/15.0+2.0/16.0)/3.0, d.Calculate(7))
	testutils.DecimalEquals(t, 100.0*(4.0/13.0+2.0/16.0+3.0/15.0)/3.0, d.Calculate(8))
	testutils.DecimalEquals(t, 100.0*(11.0/17.0+4.0/13.0+2.0/16.0)/3.0, d.Calculate(9))
	testutils.DecimalEquals(t, 100.0*(24.0/49.0+11.0/17.0+4.0/13.0)/3.0, d.Calculate(10))
}

func TestFastStochasticIndicatorNoPriceChange(t *testing.T) {
	ts := testutils.MockTimeSeriesOCHL(
		[]float64{42, 42, 42, 42},
		[]float64{42, 42, 42, 42},
	)

	k := indicators.NewFastStochasticIndicator(ts, 2)
	assert.Equal(t, decimal.New(math.Inf(1)).FormattedString(2), k.Calculate(1).FormattedString(2))
}
