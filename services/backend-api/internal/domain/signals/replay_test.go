package signals

import (
	"reflect"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestReplay_DeterministicAcrossRuns(t *testing.T) {
	cfg := Config{
		Lookback:  3,
		MinChange: decimal.RequireFromString("0.01"),
	}

	ticks := []Tick{
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100"), Timestamp: time.Unix(1000, 0)},
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("101"), Timestamp: time.Unix(1001, 0)},
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("102"), Timestamp: time.Unix(1002, 0)},
		{Symbol: "ETH/USDT", Last: decimal.RequireFromString("50"), Timestamp: time.Unix(1003, 0)},
		{Symbol: "ETH/USDT", Last: decimal.RequireFromString("49"), Timestamp: time.Unix(1004, 0)},
		{Symbol: "ETH/USDT", Last: decimal.RequireFromString("48"), Timestamp: time.Unix(1005, 0)},
	}

	runA := Replay("strategy-a", cfg, ticks)
	runB := Replay("strategy-a", cfg, ticks)

	if len(runA) == 0 {
		t.Fatal("expected replay to produce at least one signal")
	}
	if !reflect.DeepEqual(runA, runB) {
		t.Fatalf("expected deterministic replay output, runA=%v runB=%v", runA, runB)
	}
}
