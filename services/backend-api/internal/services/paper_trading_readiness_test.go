package services

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestEvaluateAcceptance_ReadyRequiresAllGatesIncludingPositivePnL(t *testing.T) {
	g := &ReadinessManifestGenerator{}

	t.Run("all gates met with positive PnL -> Ready", func(t *testing.T) {
		result := g.evaluateAcceptance(
			decimal.NewFromFloat(720.0), // 30 days
			10,                          // 10 closed trades
			1,                           // 1 strategy
			true,                        // risk limits OK
			true,                        // backtest OK
			decimal.NewFromFloat(50.0),  // positive PnL
		)
		if !result.Ready {
			t.Errorf("expected Ready=true when all gates met; failures=%v", result.Failures)
		}
		if !result.PositivePnLMet {
			t.Errorf("expected PositivePnLMet=true with positive PnL")
		}
		if !result.MinHoursMet {
			t.Errorf("expected MinHoursMet=true at 720h")
		}
	})

	t.Run("zero PnL -> Ready=false, PositivePnLMet=false", func(t *testing.T) {
		result := g.evaluateAcceptance(
			decimal.NewFromFloat(720.0),
			10,
			1,
			true,
			true,
			decimal.Zero,
		)
		if result.Ready {
			t.Errorf("expected Ready=false when PnL is zero (must be positive)")
		}
		if result.PositivePnLMet {
			t.Errorf("expected PositivePnLMet=false when PnL is zero")
		}
		foundFailure := false
		for _, f := range result.Failures {
			if contains(f, "positive") || contains(f, "PnL") {
				foundFailure = true
				break
			}
		}
		if !foundFailure {
			t.Errorf("expected a failure message mentioning positive PnL; got %v", result.Failures)
		}
	})

	t.Run("negative PnL -> Ready=false, PositivePnLMet=false", func(t *testing.T) {
		result := g.evaluateAcceptance(
			decimal.NewFromFloat(720.0),
			10,
			1,
			true,
			true,
			decimal.NewFromFloat(-50.0),
		)
		if result.Ready {
			t.Errorf("expected Ready=false when PnL is negative")
		}
		if result.PositivePnLMet {
			t.Errorf("expected PositivePnLMet=false when PnL is negative")
		}
	})

	t.Run("less than 720 hours -> Ready=false, MinHoursMet=false", func(t *testing.T) {
		result := g.evaluateAcceptance(
			decimal.NewFromFloat(168.0), // 7 days, below 30-day threshold
			10,
			1,
			true,
			true,
			decimal.NewFromFloat(50.0),
		)
		if result.Ready {
			t.Errorf("expected Ready=false at 168h (below 720h minimum)")
		}
		if result.MinHoursMet {
			t.Errorf("expected MinHoursMet=false at 168h")
		}
		// PnL is positive but hours are insufficient
		if !result.PositivePnLMet {
			t.Errorf("expected PositivePnLMet=true (positive PnL given)")
		}
	})

	t.Run("multiple failures aggregate", func(t *testing.T) {
		result := g.evaluateAcceptance(
			decimal.NewFromFloat(168.0), // hours fail
			5,                           // trades fail
			0,                           // strategies fail
			false,                       // risk fail
			false,                       // backtest fail
			decimal.NewFromFloat(-10.0), // PnL fail
		)
		if result.Ready {
			t.Errorf("expected Ready=false with all gates failing")
		}
		// All gates should be false
		if result.MinHoursMet || result.MinTradesMet || result.MinStrategiesMet ||
			result.RiskLimitsMet || result.BacktestMet || result.PositivePnLMet {
			t.Errorf("expected all gates to be false; got MinHours=%v MinTrades=%v MinStrategies=%v Risk=%v Backtest=%v PositivePnL=%v",
				result.MinHoursMet, result.MinTradesMet, result.MinStrategiesMet,
				result.RiskLimitsMet, result.BacktestMet, result.PositivePnLMet)
		}
		// Should have multiple failures
		if len(result.Failures) < 6 {
			t.Errorf("expected at least 6 failures, got %d: %v", len(result.Failures), result.Failures)
		}
	})
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
