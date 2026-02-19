package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestDerivativeIndicator(t *testing.T) {
	ts := testutils.MockTimeSeries("1", "1", "2", "3", "5", "8", "13")
	indicator := indicators.NewDerivativeIndicator(indicators.NewClosePriceIndicator(ts))

	t.Run("returns zero at index zero", func(t *testing.T) {
		assert.EqualValues(t, "0", indicator.Calculate(0).String())
	})

	t.Run("returns the derivative", func(t *testing.T) {
		assert.EqualValues(t, "0", indicator.Calculate(1).String())

		for i := 2; i < len(ts.Candles); i++ {
			// Derivative is (Calculate(index) - Calculate(index-1))
			// Fib: 1, 1, 2, 3, 5, 8, 13
			// Diff: 0, 1, 1, 2, 3, 5
			// So index 2 is 2-1 = 1.
			val1 := ts.Candles[i].ClosePrice
			val2 := ts.Candles[i-1].ClosePrice
			expected := val1.Sub(val2)

			assert.EqualValues(t, expected.String(), indicator.Calculate(i).String())
		}
	})
}
