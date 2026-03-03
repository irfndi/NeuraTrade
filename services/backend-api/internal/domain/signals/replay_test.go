package signals

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplay_DeterministicAcrossRuns(t *testing.T) {
	testCases := []struct {
		name  string
		cfg   Config
		ticks []Tick
	}{
		{
			name: "mixed symbols deterministic output",
			cfg: Config{
				Lookback:  3,
				MinChange: decimal.RequireFromString("0.01"),
			},
			ticks: []Tick{
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100"), Timestamp: time.Unix(1000, 0)},
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("101"), Timestamp: time.Unix(1001, 0)},
				{Symbol: "BTC/USDT", Last: decimal.RequireFromString("102"), Timestamp: time.Unix(1002, 0)},
				{Symbol: "ETH/USDT", Last: decimal.RequireFromString("50"), Timestamp: time.Unix(1003, 0)},
				{Symbol: "ETH/USDT", Last: decimal.RequireFromString("49"), Timestamp: time.Unix(1004, 0)},
				{Symbol: "ETH/USDT", Last: decimal.RequireFromString("48"), Timestamp: time.Unix(1005, 0)},
			},
		},
		{
			name: "single symbol deterministic output",
			cfg: Config{
				Lookback:  3,
				MinChange: decimal.RequireFromString("0.02"),
			},
			ticks: []Tick{
				{Symbol: "SOL/USDT", Last: decimal.RequireFromString("10"), Timestamp: time.Unix(1000, 0)},
				{Symbol: "SOL/USDT", Last: decimal.RequireFromString("10.5"), Timestamp: time.Unix(1001, 0)},
				{Symbol: "SOL/USDT", Last: decimal.RequireFromString("11"), Timestamp: time.Unix(1002, 0)},
				{Symbol: "SOL/USDT", Last: decimal.RequireFromString("11.5"), Timestamp: time.Unix(1003, 0)},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runA := Replay("strategy-a", tc.cfg, tc.ticks)
			runB := Replay("strategy-a", tc.cfg, tc.ticks)

			require.NotEmpty(t, runA, "expected replay to produce at least one signal")
			assert.Equal(t, runA, runB)
		})
	}
}
