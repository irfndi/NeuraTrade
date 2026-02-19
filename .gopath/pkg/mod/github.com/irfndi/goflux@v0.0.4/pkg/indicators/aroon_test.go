package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestAroonUpIndicator(t *testing.T) {
	t.Run("with < window periods", func(t *testing.T) {
		ts := series.NewTimeSeries()
		indicator := indicators.NewHighPriceIndicator(ts)

		aroonUp := indicators.NewAroonUpIndicator(indicator, 10)
		testutils.DecimalEquals(t, 0, aroonUp.Calculate(0))
	})

	t.Run("with > window periods", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(1, 2, 3, 4, 3, 2, 1)
		indicator := indicators.NewHighPriceIndicator(ts)

		aroonUpIndicator := indicators.NewAroonUpIndicator(indicator, 4)

		testutils.DecimalEquals(t, 100, aroonUpIndicator.Calculate(3))
		testutils.DecimalEquals(t, 75, aroonUpIndicator.Calculate(4))
		testutils.DecimalEquals(t, 50, aroonUpIndicator.Calculate(5))
	})
}

func TestAroonDownIndicator(t *testing.T) {
	t.Run("with < window periods", func(t *testing.T) {
		ts := series.NewTimeSeries()
		indicator := indicators.NewHighPriceIndicator(ts)

		aroonUp := indicators.NewAroonDownIndicator(indicator, 10)
		testutils.DecimalEquals(t, 0, aroonUp.Calculate(0))
	})

	t.Run("with > window periods", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(5, 4, 3, 2, 3, 4, 5)
		indicator := indicators.NewLowPriceIndicator(ts)

		aroonUpIndicator := indicators.NewAroonDownIndicator(indicator, 4)

		testutils.DecimalEquals(t, 100, aroonUpIndicator.Calculate(3))
		testutils.DecimalEquals(t, 75, aroonUpIndicator.Calculate(4))
		testutils.DecimalEquals(t, 50, aroonUpIndicator.Calculate(5))
	})
}
