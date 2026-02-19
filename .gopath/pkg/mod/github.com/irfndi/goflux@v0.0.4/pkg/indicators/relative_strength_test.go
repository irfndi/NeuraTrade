package indicators_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestRelativeStrengthIndexIndicator(t *testing.T) {
	indicator := indicators.NewRelativeStrengthIndexIndicator(indicators.NewClosePriceIndicator(testutils.MockedTimeSeries), 3)

	expectedValues := []float64{
		0,
		0,
		0,
		0,
		0,
		0,
		57.9952,
		54.0751,
		21.451,
		44.7739,
		14.1542,
		21.2794,
	}

	testutils.IndicatorEquals(t, expectedValues, indicator)
}

func TestRelativeStrengthIndicator(t *testing.T) {
	indicator := indicators.NewRelativeStrengthIndicator(indicators.NewClosePriceIndicator(testutils.MockedTimeSeries), 3)

	expectedValues := []float64{
		0,
		0,
		0,
		0,
		0,
		0,
		1.3807,
		1.1775,
		0.2731,
		0.8107,
		0.1649,
		0.2703,
	}

	testutils.IndicatorEquals(t, expectedValues, indicator)
}

func TestRelativeStrengthIndicatorNoPriceChange(t *testing.T) {
	close := indicators.NewClosePriceIndicator(testutils.MockTimeSeries("42.0", "42.0"))
	rsInd := indicators.NewRelativeStrengthIndicator(close, 2)
	assert.Equal(t, decimal.New(math.Inf(1)).FormattedString(2), rsInd.Calculate(1).FormattedString(2))
}
