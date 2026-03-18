package services

import (
	"context"
	"sync"
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

func TestShadowEvaluationCoordinatorConcurrentAccess(t *testing.T) {
	coordinator := NewShadowEvaluationCoordinator(nil, nil, nil, nil, []ShadowVariantConfig{
		NewDefaultShadowVariant(),
	})
	entry := decimal.NewFromInt(100)
	live := &AITradingDecision{
		Action:      "buy",
		Symbol:      "ETH/USDT",
		Confidence:  0.7,
		SizePercent: 1.0,
		EntryPrice:  &entry,
	}
	policy := appautonomy.ScalpingCyclePolicy{
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 1.0,
	}
	portfolio := TradingPortfolio{USDTBalanceDecimal: decimal.NewFromInt(1000)}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = coordinator.MirrorDecision(context.Background(), live, portfolio, policy)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			prices := map[string]decimal.Decimal{"ETH/USDT": decimal.NewFromInt(105)}
			sell := &AITradingDecision{Action: "sell", Symbol: "ETH/USDT"}
			coordinator.RecordShadowOutcome(context.Background(), sell, prices)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = coordinator.shadowMetricsSnapshot()
		}()
	}
	wg.Wait()
}

func TestShadowEvaluationCoordinatorCloseStalePositions(t *testing.T) {
	deterministic := DefaultPaperExecutionConfig()
	deterministic.EnableRandomness = false
	coordinator := NewShadowEvaluationCoordinator(nil, nil, NewPaperExecutionSimulator(deterministic), nil, []ShadowVariantConfig{
		NewDefaultShadowVariant(),
	})
	entry := decimal.NewFromInt(100)
	live := &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		Confidence:  0.7,
		SizePercent: 1.0,
		EntryPrice:  &entry,
	}
	policy := appautonomy.ScalpingCyclePolicy{
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 1.0,
	}
	portfolio := TradingPortfolio{USDTBalanceDecimal: decimal.NewFromInt(1000)}

	_, _ = coordinator.MirrorDecision(context.Background(), live, portfolio, policy)

	runtime := coordinator.runtimeForVariant("baseline")
	runtime.mu.Lock()
	if _, ok := runtime.openDecisions["BTC/USDT"]; !ok {
		runtime.mu.Unlock()
		t.Fatalf("expected open decision for BTC/USDT after buy")
	}
	runtime.mu.Unlock()

	prices := map[string]decimal.Decimal{"BTC/USDT": decimal.NewFromInt(105)}
	maxAge := time.Now().UTC().Add(-time.Nanosecond)
	coordinator.CloseStaleShadowPositions(context.Background(), prices, maxAge)

	runtime.mu.Lock()
	if _, ok := runtime.openDecisions["BTC/USDT"]; ok {
		runtime.mu.Unlock()
		t.Fatalf("expected stale position to be closed")
	}
	runtime.mu.Unlock()
}
