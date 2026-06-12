package services

import (
	"testing"
)

func TestApplyDeterministicFallbackConfigFromEnv_ReadsNewHelperFields(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_REVERSAL_BUY_MAX_SPREAD_PCT", "0.15")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_REVERSAL_BUY_MAX_RECENT_PCT", "0.5")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_REVERSAL_BUY_MAX_TREND_PCT", "0.5")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MAX_RANGE_PCT", "85.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MAX_SPREAD_PCT", "0.20")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MAX_IMBALANCE", "-0.05")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MIN_RECENT_PCT", "-1.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MAX_RECENT_PCT", "1.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MIN_TREND_PCT", "-1.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SELL_WINDOW_MAX_TREND_PCT", "1.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BLOWOFF_SELL_TREND_MIN_PCT", "0.10")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BLOWOFF_SELL_RECENT_MIN_PCT", "0.20")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BLOWOFF_SELL_MAX_IMBALANCE", "-0.10")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_NO_RECENT_BUY_MAX_RANGE_PCT", "60.0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BT_IMBALANCE_FLOOR", "0.02")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BT_STRONG_IMBALANCE_FLOOR", "0.10")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_BT_RANGE_BUFFER_PCT", "12.0")

	got := applyDeterministicFallbackConfigFromEnv(DefaultDeterministicFallbackConfig()).Normalized()

	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"ReversalBuyMaxSpreadPct", got.ReversalBuyMaxSpreadPct, 0.15},
		{"ReversalBuyMaxRecentPct", got.ReversalBuyMaxRecentPct, 0.5},
		{"ReversalBuyMaxTrendPct", got.ReversalBuyMaxTrendPct, 0.5},
		{"SellWindowMaxRangePct", got.SellWindowMaxRangePct, 85.0},
		{"SellWindowMaxSpreadPct", got.SellWindowMaxSpreadPct, 0.20},
		{"SellWindowMaxImbalance", got.SellWindowMaxImbalance, -0.05},
		{"SellWindowMinRecentPct", got.SellWindowMinRecentPct, -1.0},
		{"SellWindowMaxRecentPct", got.SellWindowMaxRecentPct, 1.0},
		{"SellWindowMinTrendPct", got.SellWindowMinTrendPct, -1.0},
		{"SellWindowMaxTrendPct", got.SellWindowMaxTrendPct, 1.0},
		{"BlowoffSellTrendMinPct", got.BlowoffSellTrendMinPct, 0.10},
		{"BlowoffSellRecentMinPct", got.BlowoffSellRecentMinPct, 0.20},
		{"BlowoffSellMaxImbalance", got.BlowoffSellMaxImbalance, -0.10},
		{"NoRecentBuyMaxRangePct", got.NoRecentBuyMaxRangePct, 60.0},
		{"BacktestImbalanceFloor", got.BacktestImbalanceFloor, 0.02},
		{"BacktestStrongImbalanceFloor", got.BacktestStrongImbalanceFloor, 0.10},
		{"BacktestRangeBufferPct", got.BacktestRangeBufferPct, 12.0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestScalpingBlowoffSellTrendConfirmed_Live(t *testing.T) {
	fallback := DefaultDeterministicFallbackConfig().Normalized()

	t.Run("all conditions met returns true", func(t *testing.T) {
		signal := aiMarketSignal{
			RecentChangeKnown:  true,
			PriceChange24h:     0.10,
			RecentPriceChange:  0.20,
			OrderBookImbalance: -0.40,
		}
		if !scalpingBlowoffSellTrendConfirmed(signal, fallback) {
			t.Error("expected true when all blowoff conditions met")
		}
	})

	t.Run("missing recent change returns false", func(t *testing.T) {
		signal := aiMarketSignal{
			RecentChangeKnown:  false,
			PriceChange24h:     0.10,
			RecentPriceChange:  0.20,
			OrderBookImbalance: -0.40,
		}
		if scalpingBlowoffSellTrendConfirmed(signal, fallback) {
			t.Error("expected false when RecentChangeKnown is false")
		}
	})

	t.Run("trend below threshold returns false", func(t *testing.T) {
		signal := aiMarketSignal{
			RecentChangeKnown:  true,
			PriceChange24h:     0.01,
			RecentPriceChange:  0.20,
			OrderBookImbalance: -0.40,
		}
		if scalpingBlowoffSellTrendConfirmed(signal, fallback) {
			t.Error("expected false when PriceChange24h below threshold")
		}
	})

	t.Run("imbalance not negative enough returns false", func(t *testing.T) {
		signal := aiMarketSignal{
			RecentChangeKnown:  true,
			PriceChange24h:     0.10,
			RecentPriceChange:  0.20,
			OrderBookImbalance: -0.10,
		}
		if scalpingBlowoffSellTrendConfirmed(signal, fallback) {
			t.Error("expected false when OrderBookImbalance above threshold")
		}
	})
}

func TestScalpingReversalBuyCandidate_RespectsFallback(t *testing.T) {
	tight := DefaultDeterministicFallbackConfig().Normalized()
	if scalpingReversalBuyCandidate(aiMarketSignal{
		RecentChangeKnown: true,
		BidAskSpread:      0.10,
		RangePosition24h:  10.0,
		RecentPriceChange: -0.20,
		PriceChange24h:    -0.10,
	}, tight) {
		t.Error("expected default ReversalBuyMaxSpreadPct=0.06 to reject BidAskSpread=0.10")
	}

	wide := tight
	wide.ReversalBuyMaxSpreadPct = 0.20
	if !scalpingReversalBuyCandidate(aiMarketSignal{
		RecentChangeKnown: true,
		BidAskSpread:      0.10,
		RangePosition24h:  10.0,
		RecentPriceChange: -0.20,
		PriceChange24h:    -0.10,
	}, wide) {
		t.Error("expected widened ReversalBuyMaxSpreadPct=0.20 to accept BidAskSpread=0.10")
	}
}

func TestScalpingSellWindowCandidate_RespectsFallback(t *testing.T) {
	tight := DefaultDeterministicFallbackConfig().Normalized()
	if scalpingSellWindowCandidate(aiMarketSignal{
		RecentChangeKnown:  true,
		BidAskSpread:       0.15,
		OrderBookImbalance: -0.40,
		RangePosition24h:   30.0,
		RecentPriceChange:  -0.10,
		PriceChange24h:     0.10,
	}, tight) {
		t.Error("expected default SellWindowMaxSpreadPct=0.10 to reject BidAskSpread=0.15")
	}

	wide := tight
	wide.SellWindowMaxSpreadPct = 0.20
	if !scalpingSellWindowCandidate(aiMarketSignal{
		RecentChangeKnown:  true,
		BidAskSpread:       0.15,
		OrderBookImbalance: -0.40,
		RangePosition24h:   30.0,
		RecentPriceChange:  -0.10,
		PriceChange24h:     0.10,
	}, wide) {
		t.Error("expected widened SellWindowMaxSpreadPct=0.20 to accept BidAskSpread=0.15")
	}
}
