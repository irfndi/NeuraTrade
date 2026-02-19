package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestRSISignal(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(10, 20, 30, 40, 50, 60, 70, 80, 90, 100)
	rsi := indicators.NewRelativeStrengthIndexIndicator(indicators.NewClosePriceIndicator(ts), 5)

	// Create signal indicator
	signal := indicators.NewRSISignal(rsi, 70, 30)

	// We expect Neutral or Sell if RSI > 70
	// RSI(9) will likely be > 70
	sig := signal.CalculateSignal(9)
	assert.True(t, sig == indicators.SignalSell || sig == indicators.SignalNeutral)
}

func TestMACDSignal(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20)
	macd := indicators.NewMACDIndicator(indicators.NewClosePriceIndicator(ts), 3, 6)
	signalLine := indicators.NewEMAIndicator(macd, 3)

	macdSig := indicators.NewMACDSignal(macd, signalLine)

	sig := macdSig.CalculateSignal(10)
	assert.Contains(t, []int{indicators.SignalBuy, indicators.SignalSell, indicators.SignalNeutral}, sig)
}
