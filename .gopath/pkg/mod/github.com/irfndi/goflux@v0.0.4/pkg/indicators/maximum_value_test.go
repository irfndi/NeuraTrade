package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestMaximumValueIndicator(t *testing.T) {
	t.Run("with window", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(-1, 10, 21, 20, 1, 4)

		mvi := indicators.NewMaximumValueIndicator(indicators.NewClosePriceIndicator(ts), 3)
		testutils.DecimalEquals(t, 20, mvi.Calculate(ts.LastIndex()))
		testutils.DecimalEquals(t, 21, mvi.Calculate(ts.LastIndex()-1))
	})

	t.Run("without window", func(t *testing.T) {
		ts := testutils.MockTimeSeriesFl(-1, 10, 0, 20, 1, 4)

		mvi := indicators.NewMaximumValueIndicator(indicators.NewClosePriceIndicator(ts), -1)
		testutils.DecimalEquals(t, 20, mvi.Calculate(ts.LastIndex()))
	})
}
