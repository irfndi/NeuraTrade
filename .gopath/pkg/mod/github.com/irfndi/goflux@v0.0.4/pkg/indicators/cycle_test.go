package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestHilbertTransform(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105, 106, 107, 108)
	closeInd := indicators.NewClosePriceIndicator(ts)
	ht := indicators.NewHilbertTransform(closeInd)

	val := ht.Calculate(8)
	assert.NotNil(t, val)
}

func TestHTTrendline(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113)
	closeInd := indicators.NewClosePriceIndicator(ts)
	htt := indicators.NewHTTrendline(closeInd)

	val := htt.Calculate(13)
	assert.NotNil(t, val)
	assert.True(t, val.GT(testutils.MockTimeSeriesFl(100).Candles[0].ClosePrice))
}

func TestNewDominantCyclePeriod(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105, 106, 107, 108)
	closeInd := indicators.NewClosePriceIndicator(ts)
	dcp := indicators.NewDominantCyclePeriod(closeInd)

	assert.NotNil(t, dcp)
}

func TestDominantCyclePeriod_Calculate(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105, 106, 107, 108)
	closeInd := indicators.NewClosePriceIndicator(ts)
	dcp := indicators.NewDominantCyclePeriod(closeInd)

	// Test index less than 7 (returns ZERO)
	val := dcp.Calculate(5)
	assert.Equal(t, val.String(), "0")

	// Test index greater than 7 (returns default 20)
	val = dcp.Calculate(8)
	assert.NotNil(t, val)
	assert.Greater(t, val.Float(), 0.0)
}
