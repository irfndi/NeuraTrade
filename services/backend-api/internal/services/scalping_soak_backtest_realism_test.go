package services

import "testing"

func TestResolveSoakBacktestSlippagePct_DefaultsToConstant(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_SLIPPAGE_PCT", "")
	if got := resolveSoakBacktestSlippagePct(); got != DefaultScalpingBacktestSlippage {
		t.Errorf("expected default %v when env unset, got %v", DefaultScalpingBacktestSlippage, got)
	}
}

func TestResolveSoakBacktestSlippagePct_RespectsOverride(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_SLIPPAGE_PCT", "0.0001")
	want := 0.0001
	if got := resolveSoakBacktestSlippagePct(); got != want {
		t.Errorf("expected %v when env set to 0.0001, got %v", want, got)
	}
}

func TestResolveSoakBacktestSlippagePct_NegativeIgnored(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_SLIPPAGE_PCT", "-0.1")
	if got := resolveSoakBacktestSlippagePct(); got != DefaultScalpingBacktestSlippage {
		t.Errorf("expected default %v when env set to negative, got %v", DefaultScalpingBacktestSlippage, got)
	}
}

func TestResolveSoakBacktestSlippagePct_ZeroAllowed(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_SLIPPAGE_PCT", "0")
	if got := resolveSoakBacktestSlippagePct(); got != 0 {
		t.Errorf("expected 0 when env set to 0 (diagnostic bypass), got %v", got)
	}
}

func TestResolveSoakBacktestNoisePct_DefaultsToZero(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_NOISE_PCT", "")
	if got := resolveSoakBacktestNoisePct(); got != 0 {
		t.Errorf("expected default 0 when env unset, got %v", got)
	}
}

func TestResolveSoakBacktestNoisePct_RespectsOverride(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_NOISE_PCT", "0.005")
	want := 0.005
	if got := resolveSoakBacktestNoisePct(); got != want {
		t.Errorf("expected %v when env set to 0.005, got %v", want, got)
	}
}

func TestResolveSoakBacktestNoisePct_NegativeIgnored(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_BACKTEST_NOISE_PCT", "-0.1")
	if got := resolveSoakBacktestNoisePct(); got != 0 {
		t.Errorf("expected default 0 when env set to negative, got %v", got)
	}
}
