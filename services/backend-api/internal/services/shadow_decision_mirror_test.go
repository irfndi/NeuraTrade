package services

import (
	"testing"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
)

func TestShadowDecisionMirrorBlocksStricterConfidence(t *testing.T) {
	mirror := NewShadowDecisionMirror(nil)
	live := &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		Confidence:  0.66,
		SizePercent: 1.5,
	}
	policy := appautonomy.ScalpingCyclePolicy{EffectiveMinConfidence: 0.60, EffectiveMaxCapitalPct: 2.0}
	variant := ShadowVariantConfig{
		VariantID: "strict",
		PolicyOverrides: map[string]interface{}{
			ShadowPolicyMinConfidence: 0.70,
		},
	}
	result := mirror.MirrorDecision(live, TradingPortfolio{}, policy, variant)
	if result.GateAllowed {
		t.Fatalf("expected strict variant to block decision")
	}
	if result.ShadowAction != "hold" {
		t.Fatalf("expected shadow hold when blocked, got %q", result.ShadowAction)
	}
}

func TestShadowDecisionMirrorReducesSizeByVariantCap(t *testing.T) {
	mirror := NewShadowDecisionMirror(nil)
	live := &AITradingDecision{
		Action:      "buy",
		Symbol:      "ETH/USDT",
		Confidence:  0.80,
		SizePercent: 3.0,
	}
	policy := appautonomy.ScalpingCyclePolicy{EffectiveMinConfidence: 0.60, EffectiveMaxCapitalPct: 5.0}
	variant := ShadowVariantConfig{
		VariantID: "cap",
		PolicyOverrides: map[string]interface{}{
			ShadowPolicyMaxCapitalPct: 1.0,
		},
	}
	result := mirror.MirrorDecision(live, TradingPortfolio{}, policy, variant)
	if !result.GateAllowed {
		t.Fatalf("expected variant to allow decision")
	}
	if result.SizePercent != 1.0 {
		t.Fatalf("expected size capped at 1.0, got %.2f", result.SizePercent)
	}
}
