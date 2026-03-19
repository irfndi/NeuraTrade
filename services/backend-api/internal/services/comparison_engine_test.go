package services

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestLiveShadowComparisonEngineBuildReport(t *testing.T) {
	engine := NewLiveShadowComparisonEngine(nil)
	live := LiveShadowMetrics{
		PnL:            decimal.NewFromInt(10),
		WinRate:        decimal.NewFromFloat(58),
		TradeCount:     20,
		RejectionCount: 5,
	}
	shadow := []ShadowVariantMetrics{
		{
			VariantID:       "aggressive",
			VariantName:     "Aggressive",
			PnL:             decimal.NewFromInt(14),
			WinRate:         decimal.NewFromFloat(62),
			TradeCount:      24,
			RejectionCount:  3,
			EntryTimingBps:  decimal.NewFromFloat(2.1),
			ExitTimingBps:   decimal.NewFromFloat(1.4),
			OpportunityCost: decimal.NewFromFloat(0.8),
		},
	}
	report := engine.BuildReport(time.Now().UTC().Add(-time.Hour), time.Now().UTC(), live, shadow)
	if len(report.Comparisons) != 1 {
		t.Fatalf("expected 1 comparison, got %d", len(report.Comparisons))
	}
	comparison := report.Comparisons[0]
	if !comparison.PnLDivergence.Equal(decimal.NewFromInt(4)) {
		t.Fatalf("expected pnl divergence 4, got %s", comparison.PnLDivergence.String())
	}
	if !comparison.OutperformingBaseline {
		t.Fatalf("expected outperforming baseline")
	}
}
