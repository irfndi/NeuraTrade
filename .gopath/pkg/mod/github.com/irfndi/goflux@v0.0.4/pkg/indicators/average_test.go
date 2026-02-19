package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestAverageGainsIndicator(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(1, 2, 3, 5, 8, 13)

		avgGains := indicators.NewAverageGainsIndicator(indicators.NewClosePriceIndicator(ts), 6)

		testutils.DecimalEquals(t, 0, avgGains.Calculate(0))
		testutils.DecimalEquals(t, 1.0/2.0, avgGains.Calculate(1))
		testutils.DecimalEquals(t, 2.0/3.0, avgGains.Calculate(2))
		testutils.DecimalEquals(t, 1.0, avgGains.Calculate(3))
		testutils.DecimalEquals(t, 7.0/5.0, avgGains.Calculate(4))
		testutils.DecimalEquals(t, 12.0/6.0, avgGains.Calculate(5))
	})

	t.Run("Oscillating indicator", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(0, 5, 2, 10, 12, 11)

		cumGains := indicators.NewAverageGainsIndicator(indicators.NewClosePriceIndicator(ts), 6)

		testutils.DecimalEquals(t, 0, cumGains.Calculate(0))
		testutils.DecimalEquals(t, 5/2.0, cumGains.Calculate(1))
		testutils.DecimalEquals(t, 5/3.0, cumGains.Calculate(2))
		testutils.DecimalEquals(t, 13.0/4.0, cumGains.Calculate(3))
		testutils.DecimalEquals(t, 15.0/5.0, cumGains.Calculate(4))
		testutils.DecimalEquals(t, 15.0/6.0, cumGains.Calculate(5))
	})

	t.Run("Rolling window", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(0, 5, 2, 10, 12, 11)

		cumGains := indicators.NewAverageGainsIndicator(indicators.NewClosePriceIndicator(ts), 3)

		testutils.DecimalEquals(t, 0, cumGains.Calculate(0))
		testutils.DecimalEquals(t, 5.0/2.0, cumGains.Calculate(1))
		testutils.DecimalEquals(t, 5.0/3.0, cumGains.Calculate(2))
		testutils.DecimalEquals(t, 13.0/3.0, cumGains.Calculate(3))
		testutils.DecimalEquals(t, 10.0/3.0, cumGains.Calculate(4))
		testutils.DecimalEquals(t, 10.0/3.0, cumGains.Calculate(5))
	})
}

func TestNewAverageLossesIndicator(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(13, 8, 5, 3, 2, 1)

		cumLosses := indicators.NewAverageLossesIndicator(indicators.NewClosePriceIndicator(ts), 6)

		testutils.DecimalEquals(t, 0, cumLosses.Calculate(0))
		testutils.DecimalEquals(t, 5.0/2.0, cumLosses.Calculate(1))
		testutils.DecimalEquals(t, 8.0/3.0, cumLosses.Calculate(2))
		testutils.DecimalEquals(t, 10.0/4.0, cumLosses.Calculate(3))
		testutils.DecimalEquals(t, 11.0/5.0, cumLosses.Calculate(4))
		testutils.DecimalEquals(t, 12.0/6.0, cumLosses.Calculate(5))
	})

	t.Run("Oscillating indicator", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(13, 16, 10, 8, 9, 8)

		cumLosses := indicators.NewAverageLossesIndicator(indicators.NewClosePriceIndicator(ts), 6)

		testutils.DecimalEquals(t, 0, cumLosses.Calculate(0))
		testutils.DecimalEquals(t, 0, cumLosses.Calculate(1))
		testutils.DecimalEquals(t, 6.0/3.0, cumLosses.Calculate(2))
		testutils.DecimalEquals(t, 8.0/4.0, cumLosses.Calculate(3))
		testutils.DecimalEquals(t, 8.0/5.0, cumLosses.Calculate(4))
		testutils.DecimalEquals(t, 9.0/6.0, cumLosses.Calculate(5))
	})

	t.Run("Rolling window", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(13, 16, 10, 8, 9, 8)

		cumLosses := indicators.NewAverageLossesIndicator(indicators.NewClosePriceIndicator(ts), 3)

		testutils.DecimalEquals(t, 0, cumLosses.Calculate(0))
		testutils.DecimalEquals(t, 0, cumLosses.Calculate(1))
		testutils.DecimalEquals(t, 6.0/3.0, cumLosses.Calculate(2))
		testutils.DecimalEquals(t, 8.0/3.0, cumLosses.Calculate(3))
		testutils.DecimalEquals(t, 8.0/3.0, cumLosses.Calculate(4))
		testutils.DecimalEquals(t, 1.0, cumLosses.Calculate(5))
	})
}
