package indicators_test

import (
	"testing"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestModifiedMovingAverage(t *testing.T) {
	indicator := indicators.NewMMAIndicator(indicators.NewClosePriceIndicator(testutils.MockedTimeSeries), 3)

	expected := []float64{
		0,
		0,
		64.09,
		63.97,
		63.83,
		63.6167,
		63.7144,
		63.7596,
		63.4898,
		63.4498,
		62.7432,
		62.3321,
	}

	testutils.IndicatorEquals(t, expected, indicator)
}
