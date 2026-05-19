package services

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/shopspring/decimal"
)

// ScalpingRolloutMetricsFromSoak converts accepted paper-soak evidence into
// the rollout metrics consumed by the scalping live-mode proof gate.
func ScalpingRolloutMetricsFromSoak(result *ScalpingLivePaperSoakResult) (autonomous.RolloutMetrics, error) {
	if result == nil {
		return autonomous.RolloutMetrics{}, fmt.Errorf("scalping soak result is required")
	}
	report := result.Report
	holdRatio := decimal.Zero
	if value, ok := report.ActionSplit["hold"]; ok {
		holdRatio = value
	}
	maxDrawdown := report.TradeSummary.MaxDrawdownPct
	if !maxDrawdown.GreaterThan(decimal.Zero) {
		maxDrawdown = report.TradeSummary.MaxDrawdown
	}
	return autonomous.RolloutMetrics{
		TotalTrades:              report.TradeSummary.ClosedTrades,
		WinningTrades:            report.TradeSummary.Wins,
		LosingTrades:             report.TradeSummary.Losses,
		TotalPnL:                 report.TradeSummary.NetPnL,
		WinRate:                  report.TradeSummary.WinRate.InexactFloat64(),
		MaxDrawdown:              maxDrawdown,
		SignalQualityCoverage:    report.SignalQuality.Coverage,
		AIProviderDegradedCycles: report.AIProviderDegradation.DegradedCycles,
		HoldRatio:                holdRatio,
		OpenPositions:            result.OpenPositions,
		LastUpdated:              time.Now().UTC(),
	}, nil
}

// RecordScalpingLiveTrialProof persists live-ready paper-soak proof metrics into
// the autonomy rollout state for the requested strategy.
func RecordScalpingLiveTrialProof(
	ctx context.Context,
	db *sql.DB,
	strategyID string,
	result *ScalpingLivePaperSoakResult,
) (*autonomous.RolloutState, error) {
	if db == nil {
		return nil, fmt.Errorf("record scalping live trial proof requires database")
	}
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategy_id is required")
	}
	if result == nil {
		return nil, fmt.Errorf("scalping soak result is required")
	}
	if !result.Report.LiveTrialReadiness.Ready {
		return nil, fmt.Errorf("scalping live trial proof not ready: %s", strings.Join(result.Report.LiveTrialReadiness.Reasons, ", "))
	}

	metrics, err := ScalpingRolloutMetricsFromSoak(result)
	if err != nil {
		return nil, err
	}
	if failures := scalpingLiveProofMetricFailures(metrics); len(failures) > 0 {
		return nil, fmt.Errorf("scalping live trial proof metrics invalid: %s", strings.Join(failures, ", "))
	}
	store := NewAutonomousRolloutStore(db)
	if err := store.InitSchema(ctx); err != nil {
		return nil, fmt.Errorf("initialize autonomous rollout schema: %w", err)
	}
	state, err := store.GetRolloutState(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("get rollout state: %w", err)
	}
	if state == nil {
		state = &autonomous.RolloutState{
			StrategyID:        strategyID,
			CurrentStage:      autonomous.StagePaper,
			Status:            autonomous.StatusActive,
			EnteredAt:         time.Now().UTC(),
			PromotionCriteria: autonomous.DefaultPromotionCriteria(),
			Metrics:           metrics,
			History:           nil,
		}
	} else {
		state.Metrics = metrics
	}
	if state.CurrentStage == "" {
		state.CurrentStage = autonomous.StagePaper
	}
	if state.Status == "" {
		state.Status = autonomous.StatusActive
	}
	if state.EnteredAt.IsZero() {
		state.EnteredAt = time.Now().UTC()
	}
	if err := store.SaveRolloutState(ctx, state); err != nil {
		return nil, fmt.Errorf("save rollout state: %w", err)
	}
	return state, nil
}
