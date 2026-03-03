package signals

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestEngineEvaluate_ProposesBuySignal(t *testing.T) {
	engine := NewEngine(Config{
		Lookback:  3,
		MinChange: decimal.RequireFromString("0.01"),
	})

	ticks := []Tick{
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100"), Timestamp: time.Unix(1000, 0)},
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100.5"), Timestamp: time.Unix(1001, 0)},
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("102"), Timestamp: time.Unix(1002, 0)},
	}

	got, ok := engine.Evaluate("strategy-a", ticks)
	if !ok {
		t.Fatal("expected signal to be proposed")
	}
	if got.Side != SideBuy {
		t.Fatalf("expected side %q, got %q", SideBuy, got.Side)
	}
	if got.Symbol != "BTC/USDT" {
		t.Fatalf("expected symbol BTC/USDT, got %s", got.Symbol)
	}
	if got.StrategyID != "strategy-a" {
		t.Fatalf("expected strategy ID strategy-a, got %s", got.StrategyID)
	}
}

func TestEngineEvaluate_NoSignalBelowThreshold(t *testing.T) {
	engine := NewEngine(Config{
		Lookback:  3,
		MinChange: decimal.RequireFromString("0.02"),
	})

	ticks := []Tick{
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100"), Timestamp: time.Unix(1000, 0)},
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100.3"), Timestamp: time.Unix(1001, 0)},
		{Symbol: "BTC/USDT", Last: decimal.RequireFromString("100.8"), Timestamp: time.Unix(1002, 0)},
	}

	_, ok := engine.Evaluate("strategy-a", ticks)
	if ok {
		t.Fatal("expected no signal below threshold")
	}
}
