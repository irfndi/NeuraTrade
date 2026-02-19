package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestMeanDeviationIndicator(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(1, 2, 7, 6, 3, 4, 5, 11, 3, 0, 9)

	meanDeviation := indicators.NewMeanDeviationIndicator(indicators.NewClosePriceIndicator(ts), 5)

	expected := []float64{
		0,
		0,
		0,
		0,
		2.16,
		1.68,
		1.2,
		2.16,
		2.32,
		2.72,
		3.52,
	}

	testutils.IndicatorEquals(t, expected, meanDeviation)
}
