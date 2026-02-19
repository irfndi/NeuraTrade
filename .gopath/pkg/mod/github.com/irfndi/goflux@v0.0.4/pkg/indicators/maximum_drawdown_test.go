package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestMaximumDrawdownIndicator(t *testing.T) {
	t.Run("with window", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(-1, 10, 0, 20, 1, 4)

		mvi := indicators.NewMaximumDrawdownIndicator(indicators.NewClosePriceIndicator(ts), 3)
		testutils.DecimalEquals(t, -0.95, mvi.Calculate(ts.LastIndex()))
	})

	t.Run("without window", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(-1, 10, 0, 20, 1, 4)

		mvi := indicators.NewMaximumDrawdownIndicator(indicators.NewClosePriceIndicator(ts), -1)
		testutils.DecimalEquals(t, -1.05, mvi.Calculate(ts.LastIndex()))
	})
}
