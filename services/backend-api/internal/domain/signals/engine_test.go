package signals

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineEvaluate(t *testing.T) {
	testCases := []struct {
		name         string
		config       Config
		ticks        []Tick
		expectedOK   bool
		expectedSide Side
	}{
		{
			name: "proposes buy signal",
			config: Config{
				Lookback:  3,
				MinChange: decimal.RequireFromString("0.01"),
			},
			ticks: []Tick{
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100"), Timestamp: time.Unix(1000, 0)},
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100.5"), Timestamp: time.Unix(1001, 0)},
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("102"), Timestamp: time.Unix(1002, 0)},
			},
			expectedOK:   true,
			expectedSide: SideBuy,
		},
		{
			name: "no signal below threshold",
			config: Config{
				Lookback:  3,
				MinChange: decimal.RequireFromString("0.02"),
			},
			ticks: []Tick{
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100"), Timestamp: time.Unix(1000, 0)},
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100.3"), Timestamp: time.Unix(1001, 0)},
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100.8"), Timestamp: time.Unix(1002, 0)},
			},
			expectedOK: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			engine := NewEngine(tc.config)

			got, ok := engine.Evaluate("strategy-a", tc.ticks)
			require.Equal(t, tc.expectedOK, ok)

			if tc.expectedOK {
				assert.Equal(t, tc.expectedSide, got.Side)
				assert.Equal(t, "BTC/USDT", got.Symbol)
				assert.Equal(t, "strategy-a", got.StrategyID)
				return
			}

			assert.False(t, ok)
		})
	}
}
