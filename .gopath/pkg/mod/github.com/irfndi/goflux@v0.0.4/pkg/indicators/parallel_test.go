package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestMultiCalculate(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105)
	closeInd := indicators.NewClosePriceIndicator(ts)
	sma := indicators.NewSimpleMovingAverage(closeInd, 2)
	ema := indicators.NewEMAIndicator(closeInd, 2)

	results := indicators.MultiCalculate(5, closeInd, sma, ema)

	assert.Equal(t, 3, len(results))
	assert.Equal(t, "105.00", results[0].FormattedString(2))
	assert.True(t, results[1].GT(results[0].Sub(testutils.MockTimeSeriesFl(1).Candles[0].ClosePrice))) // just a check
}
