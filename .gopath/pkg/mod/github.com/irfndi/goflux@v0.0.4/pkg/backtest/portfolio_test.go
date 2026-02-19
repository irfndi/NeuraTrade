package backtest

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestPortfolioSimulator(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 105, 110, 105, 100, 95, 100, 105, 110)

	// Signals: Buy at 0, Sell at 2, Buy at 5, Sell at 8
	signals := make([]int, ts.Length())
	signals[0] = indicators.SignalBuy
	signals[2] = indicators.SignalSell
	signals[5] = indicators.SignalBuy
	signals[8] = indicators.SignalSell

	ps := NewPortfolioSimulator(10000, 0.001, 0.001)
	result := ps.SimulateLongOnly(ts, signals)

	assert.Equal(t, 2, result.TotalTrades)
	assert.True(t, result.FinalEquity.GT(result.InitialCapital))
}
