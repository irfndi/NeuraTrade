package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestSimpleMovingAverage(t *testing.T) {
	expectedValues := []float64{
		0,
		0,
		64.09,
		63.75,
		63.67,
		63.49,
		63.55,
		63.65,
		63.57,
		63.39,
		62.55,
		62.07,
	}

	closePriceIndicator := indicators.NewClosePriceIndicator(testutils.MockedTimeSeries)

	testutils.IndicatorEquals(t, expectedValues, indicators.NewSimpleMovingAverage(closePriceIndicator, 3))
}
