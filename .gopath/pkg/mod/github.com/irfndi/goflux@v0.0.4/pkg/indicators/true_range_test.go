package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestTrueRangeIndicator(t *testing.T) {
	trueRangeIndicator := indicators.NewTrueRangeIndicator(testutils.MockedTimeSeries)

	expectedValues := []float64{
		0,
		2,
		2,
		2,
		2,
		2,
		2,
		2,
		2,
		2,
		3.04,
		2,
	}

	testutils.IndicatorEquals(t, expectedValues, trueRangeIndicator)
}
