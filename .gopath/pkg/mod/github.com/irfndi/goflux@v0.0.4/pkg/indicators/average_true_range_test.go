package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestAverageTrueRangeIndicator(t *testing.T) {
	atrIndicator := indicators.NewAverageTrueRangeIndicator(testutils.MockedTimeSeries, 3)

	expectedValues := []float64{
		0,
		0,
		0,
		2,
		2,
		2,
		2,
		2,
		2,
		2,
		2.3467,
		2.3467,
	}

	testutils.IndicatorEquals(t, expectedValues, atrIndicator)
}
