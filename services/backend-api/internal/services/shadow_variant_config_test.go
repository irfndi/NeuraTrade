package services

import (
	"testing"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
)

func TestShadowVariantConfigNormalize(t *testing.T) {
	variant, err := (ShadowVariantConfig{VariantID: "  Aggressive  ", Name: ""}).Normalize()
	if err != nil {
		t.Fatalf("expected normalize success, got error: %v", err)
	}
	if variant.VariantID != "aggressive" {
		t.Fatalf("expected normalized variant id, got %q", variant.VariantID)
	}
	if variant.Name != "aggressive" {
		t.Fatalf("expected fallback name, got %q", variant.Name)
	}
}

func TestShadowVariantConfigApplyToPolicy(t *testing.T) {
	base := appautonomy.ScalpingCyclePolicy{
		EffectiveMinConfidence: 0.70,
		EffectiveMaxCapitalPct: 1.0,
		MaxBidAskSpreadPct:     0.22,
	}
	variant := ShadowVariantConfig{
		VariantID: "v1",
		PolicyOverrides: map[string]interface{}{
			ShadowPolicyMinConfidence: 0.62,
			ShadowPolicyMaxCapitalPct: 2.5,
			ShadowPolicyMaxSpreadPct:  0.35,
		},
	}
	actual := variant.ApplyToPolicy(base)
	if actual.EffectiveMinConfidence != 0.62 {
		t.Fatalf("expected min confidence override, got %.2f", actual.EffectiveMinConfidence)
	}
	if actual.EffectiveMaxCapitalPct != 2.5 {
		t.Fatalf("expected max capital override, got %.2f", actual.EffectiveMaxCapitalPct)
	}
	if actual.MaxBidAskSpreadPct != 0.35 {
		t.Fatalf("expected spread override, got %.2f", actual.MaxBidAskSpreadPct)
	}
}

func TestShadowVariantStoreDeleteProtectedBaseline(t *testing.T) {
	store := NewShadowVariantStore(nil)
	if store.Delete("baseline") {
		t.Fatalf("baseline variant should be protected from delete")
	}
}
