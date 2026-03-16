package services

import (
	"context"
	"testing"
	"time"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/shopspring/decimal"
)

func TestShadowEvaluationCoordinatorUpsertAndDeleteVariant(t *testing.T) {
	coordinator := NewShadowEvaluationCoordinator(nil, nil, nil, nil, nil)
	_, err := coordinator.UpsertVariant(ShadowVariantConfig{VariantID: "test", Name: "Test Variant"})
	if err != nil {
		t.Fatalf("expected upsert success, got error: %v", err)
	}
	if !coordinator.DeleteVariant("test") {
		t.Fatalf("expected delete success")
	}
}

func TestShadowEvaluationCoordinatorMirrorAndCompare(t *testing.T) {
	coordinator := NewShadowEvaluationCoordinator(nil, nil, nil, nil, []ShadowVariantConfig{
		NewDefaultShadowVariant(),
		{
			VariantID: "high-risk",
			Name:      "High Risk",
			PolicyOverrides: map[string]interface{}{
				ShadowPolicyMinConfidence: 0.55,
				ShadowPolicyMaxCapitalPct: 2.0,
			},
		},
	})
	entry := decimal.NewFromInt(100)
	live := &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		Confidence:  0.7,
		SizePercent: 1.5,
		EntryPrice:  &entry,
	}
	policy := appautonomy.ScalpingCyclePolicy{
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 1.0,
	}
	portfolio := TradingPortfolio{USDTBalanceDecimal: decimal.NewFromInt(1000)}
	mirrored, err := coordinator.MirrorDecision(context.Background(), live, portfolio, policy)
	if err != nil {
		t.Fatalf("expected mirror success, got error: %v", err)
	}
	if len(mirrored) < 2 {
		t.Fatalf("expected at least 2 mirrored decisions, got %d", len(mirrored))
	}
	report, err := coordinator.CompareLiveVsShadow(context.Background(), time.Now().UTC().Add(-time.Hour), time.Now().UTC())
	if err != nil {
		t.Fatalf("expected compare success, got error: %v", err)
	}
	if len(report.Comparisons) == 0 {
		t.Fatalf("expected at least one variant comparison")
	}
}
