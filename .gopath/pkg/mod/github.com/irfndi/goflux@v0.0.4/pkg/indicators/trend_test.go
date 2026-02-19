package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestTrendIndicator(t *testing.T) {
	t.Run("returns the correct slope of the trend", func(t *testing.T) {
		tests := []struct {
			series         []float64
			expectedResult string
		}{
			{
				series:         []float64{0, 1, 2, 3},
				expectedResult: "1",
			},
			{
				series:         []float64{0, 2, 4, 6},
				expectedResult: "2",
			},
			{
				series:         []float64{5, 4, 3, 2},
				expectedResult: "-1",
			},
		}

		for _, test := range tests {
			series := testutils.MockTimeSeriesFl(test.series...)
			indicator := indicators.NewTrendlineIndicator(indicators.NewClosePriceIndicator(series), 4)

			assert.EqualValues(t, test.expectedResult, indicator.Calculate(3).String())
		}
	})

	t.Run("respects the window", func(t *testing.T) {
		series := testutils.MockTimeSeriesFl(-100, 1000, 0, 1, 2, 3)
		indicator := indicators.NewTrendlineIndicator(indicators.NewClosePriceIndicator(series), 4)
		assert.EqualValues(t, "1", indicator.Calculate(5).String())
	})

	t.Run("does not allow an index out of bounds on the low end", func(t *testing.T) {
		series := testutils.MockTimeSeriesFl(0, 1)
		indicator := indicators.NewTrendlineIndicator(indicators.NewClosePriceIndicator(series), 4)
		assert.EqualValues(t, "1", indicator.Calculate(1).String())
	})
}
