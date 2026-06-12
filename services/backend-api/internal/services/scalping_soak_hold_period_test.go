package services

import (
	"testing"
	"time"
)

func TestResolveSoakDefaultHoldPeriod_DefaultsToFourHours(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_DEFAULT_HOLD_PERIOD_SECONDS", "")
	if got := resolveSoakDefaultHoldPeriod(); got != DefaultScalpingBacktestHoldPeriod {
		t.Errorf("expected default %v when env unset, got %v", DefaultScalpingBacktestHoldPeriod, got)
	}
}

func TestResolveSoakDefaultHoldPeriod_RespectsOverride(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_DEFAULT_HOLD_PERIOD_SECONDS", "10")
	want := 10 * time.Second
	if got := resolveSoakDefaultHoldPeriod(); got != want {
		t.Errorf("expected %v when env set to 10, got %v", want, got)
	}
}

func TestResolveSoakDefaultHoldPeriod_NonPositiveIgnored(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_DEFAULT_HOLD_PERIOD_SECONDS", "0")
	if got := resolveSoakDefaultHoldPeriod(); got != DefaultScalpingBacktestHoldPeriod {
		t.Errorf("expected default %v when env set to 0, got %v", DefaultScalpingBacktestHoldPeriod, got)
	}
}
