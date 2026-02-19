package indicators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
)

func TestNewMACDIndicator(t *testing.T) {
	ts := testutils.RandomTimeSeries(100)

	macd := indicators.NewMACDIndicator(indicators.NewClosePriceIndicator(ts), 12, 26)

	assert.NotNil(t, macd)
}

func TestNewMACDHistogramIndicator(t *testing.T) {
	ts := testutils.RandomTimeSeries(100)

	macd := indicators.NewMACDIndicator(indicators.NewClosePriceIndicator(ts), 12, 26)
	macdHistogram := indicators.NewMACDHistogramIndicator(macd, 9)

	assert.NotNil(t, macdHistogram)
}

func BenchmarkMACDIndicator(b *testing.B) {
	ts := testutils.RandomTimeSeries(1000)
	closePrice := indicators.NewClosePriceIndicator(ts)
	macd := indicators.NewMACDIndicator(closePrice, 12, 26)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		macd.Calculate(999)
	}
}
