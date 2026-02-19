package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestGainIndicator(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(1, 2, 3, 3, 2, 1)

	gains := indicators.NewGainIndicator(indicators.NewClosePriceIndicator(ts))

	testutils.DecimalEquals(t, 0, gains.Calculate(0))
	testutils.DecimalEquals(t, 1, gains.Calculate(1))
	testutils.DecimalEquals(t, 1, gains.Calculate(2))
	testutils.DecimalEquals(t, 0, gains.Calculate(3))
	testutils.DecimalEquals(t, 0, gains.Calculate(4))
	testutils.DecimalEquals(t, 0, gains.Calculate(5))
}

func TestLossIndicator(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(1, 2, 3, 3, 2, 0)

	gains := indicators.NewLossIndicator(indicators.NewClosePriceIndicator(ts))

	testutils.DecimalEquals(t, 0, gains.Calculate(0))
	testutils.DecimalEquals(t, 0, gains.Calculate(1))
	testutils.DecimalEquals(t, 0, gains.Calculate(2))
	testutils.DecimalEquals(t, 0, gains.Calculate(3))
	testutils.DecimalEquals(t, 1, gains.Calculate(4))
	testutils.DecimalEquals(t, 2, gains.Calculate(5))
}

func TestCumulativeGainsIndicator(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(1, 2, 3, 5, 8, 13)

		cumGains := indicators.NewCumulativeGainsIndicator(indicators.NewClosePriceIndicator(ts), 6)

		testutils.DecimalEquals(t, 0, cumGains.Calculate(0))
		testutils.DecimalEquals(t, 1, cumGains.Calculate(1))
		testutils.DecimalEquals(t, 2, cumGains.Calculate(2))
		testutils.DecimalEquals(t, 4, cumGains.Calculate(3))
		testutils.DecimalEquals(t, 7, cumGains.Calculate(4))
		testutils.DecimalEquals(t, 12, cumGains.Calculate(5))
	})

	t.Run("Oscillating scale", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(0, 5, 2, 10, 12, 11)

		cumGains := indicators.NewCumulativeGainsIndicator(indicators.NewClosePriceIndicator(ts), 6)

		testutils.DecimalEquals(t, 0, cumGains.Calculate(0))
		testutils.DecimalEquals(t, 5, cumGains.Calculate(1))
		testutils.DecimalEquals(t, 5, cumGains.Calculate(2))
		testutils.DecimalEquals(t, 13, cumGains.Calculate(3))
		testutils.DecimalEquals(t, 15, cumGains.Calculate(4))
		testutils.DecimalEquals(t, 15, cumGains.Calculate(5))
	})

	t.Run("Rolling timeframe", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(0, 5, 2, 10, 12, 11)

		cumGains := indicators.NewCumulativeGainsIndicator(indicators.NewClosePriceIndicator(ts), 3)

		testutils.DecimalEquals(t, 0, cumGains.Calculate(0))
		testutils.DecimalEquals(t, 5, cumGains.Calculate(1))
		testutils.DecimalEquals(t, 5, cumGains.Calculate(2))
		testutils.DecimalEquals(t, 13, cumGains.Calculate(3))
		testutils.DecimalEquals(t, 10, cumGains.Calculate(4))
		testutils.DecimalEquals(t, 10, cumGains.Calculate(5))
	})
}

func TestCumulativeLossesIndicator(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(13, 8, 5, 3, 2, 1)

		cumLosses := indicators.NewCumulativeLossesIndicator(indicators.NewClosePriceIndicator(ts), 6)

		testutils.DecimalEquals(t, 0, cumLosses.Calculate(0))
		testutils.DecimalEquals(t, 5, cumLosses.Calculate(1))
		testutils.DecimalEquals(t, 8, cumLosses.Calculate(2))
		testutils.DecimalEquals(t, 10, cumLosses.Calculate(3))
		testutils.DecimalEquals(t, 11, cumLosses.Calculate(4))
		testutils.DecimalEquals(t, 12, cumLosses.Calculate(5))
	})

	t.Run("Oscillating indicator", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(13, 16, 10, 8, 9, 8)

		cumLosses := indicators.NewCumulativeLossesIndicator(indicators.NewClosePriceIndicator(ts), 6)

		testutils.DecimalEquals(t, 0, cumLosses.Calculate(0))
		testutils.DecimalEquals(t, 0, cumLosses.Calculate(1))
		testutils.DecimalEquals(t, 6, cumLosses.Calculate(2))
		testutils.DecimalEquals(t, 8, cumLosses.Calculate(3))
		testutils.DecimalEquals(t, 8, cumLosses.Calculate(4))
		testutils.DecimalEquals(t, 9, cumLosses.Calculate(5))
	})

	t.Run("Rolling timeframe", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(13, 16, 10, 8, 9, 8)

		cumLosses := indicators.NewCumulativeLossesIndicator(indicators.NewClosePriceIndicator(ts), 3)

		testutils.DecimalEquals(t, 0, cumLosses.Calculate(0))
		testutils.DecimalEquals(t, 0, cumLosses.Calculate(1))
		testutils.DecimalEquals(t, 6, cumLosses.Calculate(2))
		testutils.DecimalEquals(t, 8, cumLosses.Calculate(3))
		testutils.DecimalEquals(t, 8, cumLosses.Calculate(4))
		testutils.DecimalEquals(t, 3, cumLosses.Calculate(5))
	})
}

func TestPercentGainIndicator(t *testing.T) {
	t.Run("Up", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(1, 1.5, 2.25, 2.25)

		pgi := indicators.NewPercentChangeIndicator(indicators.NewClosePriceIndicator(ts))

		testutils.DecimalEquals(t, 0, pgi.Calculate(0))
		testutils.DecimalEquals(t, .5, pgi.Calculate(1))
		testutils.DecimalEquals(t, .5, pgi.Calculate(2))
		testutils.DecimalEquals(t, 0, pgi.Calculate(3))
	})

	t.Run("Down", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(10, 5, 2.5, 2.5)

		pgi := indicators.NewPercentChangeIndicator(indicators.NewClosePriceIndicator(ts))

		testutils.DecimalEquals(t, 0, pgi.Calculate(0))
		testutils.DecimalEquals(t, -.5, pgi.Calculate(1))
		testutils.DecimalEquals(t, -.5, pgi.Calculate(2))
		testutils.DecimalEquals(t, 0, pgi.Calculate(3))
	})
}
