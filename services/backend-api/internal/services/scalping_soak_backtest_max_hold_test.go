package services

import "testing"

func TestResolveSoakBacktestMaxHoldCandles_DefaultsToZero(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_MAX_HOLD_CANDLES", "")
	if got := resolveSoakBacktestMaxHoldCandles(); got != 0 {
		t.Errorf("expected default 0 (engine falls back to 200) when env unset, got %v", got)
	}
}

func TestResolveSoakBacktestMaxHoldCandles_RespectsOverride(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_MAX_HOLD_CANDLES", "1")
	if got := resolveSoakBacktestMaxHoldCandles(); got != 1 {
		t.Errorf("expected 1 when env set to 1 (diagnostic force-fast-close), got %v", got)
	}
}

func TestResolveSoakBacktestMaxHoldCandles_NonPositiveIgnored(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_MAX_HOLD_CANDLES", "-5")
	if got := resolveSoakBacktestMaxHoldCandles(); got != 0 {
		t.Errorf("expected default 0 when env set to negative, got %v", got)
	}
}

func TestResolveSoakBacktestMaxHoldCandles_ZeroIgnored(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_MAX_HOLD_CANDLES", "0")
	if got := resolveSoakBacktestMaxHoldCandles(); got != 0 {
		t.Errorf("expected default 0 when env set to 0, got %v", got)
	}
}
