package services

import (
	"os"
	"testing"
)

func TestApplyDeterministicFallbackConfigFromEnv_ReadsNewBBADXATRRecentFields(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BB_ENTRY_MAX_PCT", "0.50")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BB_EXIT_MIN_PCT", "0.90")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_ADX_MAX_PCT", "40.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_ATR_RATIO_MAX", "2.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_RECENT_BUY_MAX_SPREAD_PCT", "0.10")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_RECENT_BUY_MIN_TREND_PCT", "0.05")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_RECENT_BUY_MAX_RANGE_PCT", "60.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_RECENT_SELL_MIN_RANGE_PCT", "40.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_REVERSAL_BUY_MAX_RANGE_PCT", "30.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BLOWOFF_SELL_RANGE_MIN", "85.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BLOWOFF_SELL_RANGE_MAX", "99.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MIN_RANGE_PCT", "15.0")

	got := applyDeterministicFallbackConfigFromEnv(DefaultDeterministicFallbackConfig()).Normalized()

	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"BBEntryMaxPct", got.BBEntryMaxPct, 0.50},
		{"BBExitMinPct", got.BBExitMinPct, 0.90},
		{"ADXMaxPct", got.ADXMaxPct, 40.0},
		{"ATRRatioMax", got.ATRRatioMax, 2.0},
		{"RecentBuyMaxSpreadPct", got.RecentBuyMaxSpreadPct, 0.10},
		{"RecentBuyMinTrendPct", got.RecentBuyMinTrendPct, 0.05},
		{"RecentBuyMaxRangePct", got.RecentBuyMaxRangePct, 60.0},
		{"RecentSellMinRangePct", got.RecentSellMinRangePct, 40.0},
		{"ReversalBuyMaxRangePct", got.ReversalBuyMaxRangePct, 30.0},
		{"BlowoffSellRangeMin", got.BlowoffSellRangeMin, 85.0},
		{"BlowoffSellRangeMax", got.BlowoffSellRangeMax, 99.0},
		{"SellWindowMinRangePct", got.SellWindowMinRangePct, 15.0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestDefaultDeterministicFallbackConfig_ThresholdsMatchPriorConsts(t *testing.T) {
	got := DefaultDeterministicFallbackConfig().Normalized()

	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"BBEntryMaxPct", got.BBEntryMaxPct, 0.20},
		{"BBExitMinPct", got.BBExitMinPct, 0.80},
		{"ADXMaxPct", got.ADXMaxPct, 25.0},
		{"ATRRatioMax", got.ATRRatioMax, 1.5},
		{"RecentBuyMaxSpreadPct", got.RecentBuyMaxSpreadPct, 0.04},
		{"RecentBuyMinTrendPct", got.RecentBuyMinTrendPct, 0.02},
		{"RecentBuyMaxRangePct", got.RecentBuyMaxRangePct, 35.0},
		{"RecentSellMinRangePct", got.RecentSellMinRangePct, 75.0},
		{"ReversalBuyMaxRangePct", got.ReversalBuyMaxRangePct, 20.0},
		{"BlowoffSellRangeMin", got.BlowoffSellRangeMin, 95.0},
		{"BlowoffSellRangeMax", got.BlowoffSellRangeMax, 98.0},
		{"SellWindowMinRangePct", got.SellWindowMinRangePct, 25.0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("default %s = %v, want %v (must match prior package-level const)", c.name, c.got, c.want)
		}
	}
}

func TestApplyDeterministicFallbackConfigFromEnv_NoEnvUsesDefaults(t *testing.T) {
	envs := []string{
		"NEURATRADE_SCALPING_FALLBACK_BB_ENTRY_MAX_PCT",
		"NEURATRADE_SCALPING_FALLBACK_BB_EXIT_MIN_PCT",
		"NEURATRADE_SCALPING_FALLBACK_ADX_MAX_PCT",
		"NEURATRADE_SCALPING_FALLBACK_ATR_RATIO_MAX",
		"NEURATRADE_SCALPING_FALLBACK_RECENT_BUY_MAX_SPREAD_PCT",
		"NEURATRADE_SCALPING_FALLBACK_RECENT_BUY_MIN_TREND_PCT",
		"NEURATRADE_SCALPING_FALLBACK_RECENT_BUY_MAX_RANGE_PCT",
		"NEURATRADE_SCALPING_FALLBACK_RECENT_SELL_MIN_RANGE_PCT",
		"NEURATRADE_SCALPING_FALLBACK_REVERSAL_BUY_MAX_RANGE_PCT",
		"NEURATRADE_SCALPING_FALLBACK_BLOWOFF_SELL_RANGE_MIN",
		"NEURATRADE_SCALPING_FALLBACK_BLOWOFF_SELL_RANGE_MAX",
		"NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MIN_RANGE_PCT",
	}
	for _, e := range envs {
		_ = os.Unsetenv(e)
	}

	got := applyDeterministicFallbackConfigFromEnv(DeterministicFallbackConfig{}).Normalized()

	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"BBEntryMaxPct", got.BBEntryMaxPct, 0.20},
		{"BBExitMinPct", got.BBExitMinPct, 0.80},
		{"ADXMaxPct", got.ADXMaxPct, 25.0},
		{"ATRRatioMax", got.ATRRatioMax, 1.5},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("unset-env %s = %v, want %v", c.name, c.got, c.want)
		}
	}
}
