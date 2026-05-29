package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalModeService_SQLitePersistence(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "trading-mode.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	logger := logging.NewStandardLogger("error", "development")
	service := NewOperationalModeService(sqliteDB, DefaultOperationalModeConfig(), logger)
	ctx := context.Background()

	confirmations, err := service.AddConfirmation(ctx, "chat-1", "tester")
	require.NoError(t, err)
	assert.Equal(t, 1, confirmations)

	confirmations, err = service.AddConfirmation(ctx, "chat-1", "tester")
	require.NoError(t, err)
	assert.Equal(t, 2, confirmations)

	err = service.SetMode(ctx, "chat-1", OpModeLive, "tester")
	require.NoError(t, err)
	assert.Equal(t, OpModeLive, service.GetMode("chat-1"))

	reloaded := NewOperationalModeService(sqliteDB, DefaultOperationalModeConfig(), logger)
	assert.Equal(t, OpModeLive, reloaded.GetMode("chat-1"))
}

func TestOperationalModeService_DryAndPaperHelpersRemainDistinct(t *testing.T) {
	logger := logging.NewStandardLogger("error", "development")
	service := NewOperationalModeService(nil, DefaultOperationalModeConfig(), logger)
	ctx := context.Background()

	require.NoError(t, service.SetMode(ctx, "chat-dry", OpModeDry, "tester"))
	require.NoError(t, service.SetMode(ctx, "chat-paper", ModePaper, "tester"))

	assert.True(t, service.IsDry("chat-dry"))
	assert.False(t, service.IsPaper("chat-dry"))
	assert.False(t, service.IsDry("chat-paper"))
	assert.True(t, service.IsPaper("chat-paper"))
}

func TestOperationalModeService_GetModeInfo_UsesDistinctDryAndPaperLabels(t *testing.T) {
	logger := logging.NewStandardLogger("error", "development")
	service := NewOperationalModeService(nil, DefaultOperationalModeConfig(), logger)
	ctx := context.Background()

	require.NoError(t, service.SetMode(ctx, "chat-dry", OpModeDry, "tester"))
	require.NoError(t, service.SetMode(ctx, "chat-paper", ModePaper, "tester"))

	dryInfo := service.GetModeInfo("chat-dry")
	paperInfo := service.GetModeInfo("chat-paper")

	assert.Contains(t, dryInfo, "DRY MODE (Shadow/No Order Execution)")
	assert.Contains(t, dryInfo, "Strategy runs stay in shadow observation mode")
	assert.Contains(t, paperInfo, "PAPER MODE (Simulated Orders)")
	assert.Contains(t, paperInfo, "Orders are simulated through the autonomy paper stage")
}

func TestOperationalModeService_LiveModeGuardBlocksLiveTransition(t *testing.T) {
	logger := logging.NewStandardLogger("error", "development")
	service := NewOperationalModeService(nil, DefaultOperationalModeConfig(), logger)
	ctx := context.Background()
	blocked := errors.New("live mode blocked: missing daily trading proof")
	service.SetLiveModeGuard(func(context.Context, string, string) error {
		return blocked
	})

	_, err := service.AddConfirmation(ctx, "chat-guard", "tester")
	require.NoError(t, err)
	_, err = service.AddConfirmation(ctx, "chat-guard", "tester")
	require.NoError(t, err)

	err = service.SetMode(ctx, "chat-guard", OpModeLive, "tester")
	require.ErrorIs(t, err, blocked)
	assert.Equal(t, OpModeDry, service.GetMode("chat-guard"))

	require.NoError(t, service.SetMode(ctx, "chat-guard", ModePaper, "tester"))
	assert.Equal(t, ModePaper, service.GetMode("chat-guard"))
}

func TestManifestLiveModeGuardRequiresAllStrategyEvidence(t *testing.T) {
	guard := ManifestLiveModeGuard("", []string{"daily_trading", "swing_trading"})
	err := guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), LiveReadinessManifestEnv)
	assert.Contains(t, err.Error(), "daily_trading")
	assert.Contains(t, err.Error(), "swing_trading")

	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-soak/daily.json")
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "paper-soak/daily.json"},
			"swing_trading": {Ready: false, Reason: "paper_window_missing"},
			"arbitrage":     {Ready: true},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard = ManifestLiveModeGuard(manifestPath, []string{"daily_trading", "swing_trading", "arbitrage"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daily_trading=missing_evidence_metrics")
	assert.Contains(t, err.Error(), "swing_trading=paper_window_missing")
	assert.Contains(t, err.Error(), "arbitrage=missing_evidence")
}

func TestManifestLiveModeGuardQuotesManifestPathErrors(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "live readiness.json")
	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})

	err := guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"`)
	assert.Contains(t, err.Error(), "live readiness.json")
}

func TestManifestLiveModeGuardAllowsVerifiedStrategies(t *testing.T) {
	manifestDir := t.TempDir()
	paperMetrics := passingPaperReadinessEvidence()
	dailyMetrics := passingReadinessEvidence(2)
	swingMetrics := passingReadinessEvidence(2)
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-readiness.json", paperMetrics)
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-soak/daily.json", dailyMetrics)
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-soak/swing.json", swingMetrics)
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"paper_trading": {Ready: true, Evidence: "paper-readiness.json", EvidenceMetrics: paperMetrics},
			"daily_trading": {Ready: true, Evidence: "paper-soak/daily.json", EvidenceMetrics: dailyMetrics},
			"swing_trading": {Ready: true, Evidence: "paper-soak/swing.json", EvidenceMetrics: swingMetrics},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"paper_trading", "daily_trading", "swing_trading"})
	require.NoError(t, guard(context.Background(), "chat-1", "tester"))
}

func TestManifestLiveModeGuardRequiresReadableEvidenceArtifact(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "missing-evidence.json", EvidenceMetrics: passingReadinessEvidence(2)},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `daily_trading=evidence_unreadable_"missing-evidence.json"`)
}

func TestManifestLiveModeGuardRequiresJSONEvidenceArtifact(t *testing.T) {
	manifestDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "daily.txt"), []byte("verified"), 0o600))
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "daily.txt", EvidenceMetrics: passingReadinessEvidence(2)},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `daily_trading=evidence_invalid_json_"daily.txt"`)
}

func TestManifestLiveModeGuardRequiresObjectEvidenceArtifact(t *testing.T) {
	manifestDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "daily.json"), []byte(`"verified"`), 0o600))
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "daily.json", EvidenceMetrics: passingReadinessEvidence(2)},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `daily_trading=evidence_not_json_object_"daily.json"`)
}

func TestManifestLiveModeGuardRequiresEvidenceArtifactMetrics(t *testing.T) {
	manifestDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "daily.json"), []byte(`{"verified":true}`), 0o600))
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "daily.json", EvidenceMetrics: passingReadinessEvidence(2)},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `daily_trading=evidence_missing_metrics_"daily.json"`)
}

func TestManifestLiveModeGuardRequiresEvidenceArtifactMetricsToMatchManifest(t *testing.T) {
	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "daily.json", passingReadinessEvidence(3))
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "daily.json", EvidenceMetrics: passingReadinessEvidence(2)},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `daily_trading=evidence_metrics_mismatch_"daily.json"`)
}

func TestManifestLiveModeGuardMatchesEvidenceArtifactDecimalMetricsByValue(t *testing.T) {
	manifestDir := t.TempDir()
	manifestMetrics := passingReadinessEvidence(2)
	artifactMetrics := *manifestMetrics
	artifactMetrics.NetPnL = " 1.250 "
	artifactMetrics.AvgNetPnL = "0.100"
	artifactMetrics.MaxDrawdownPct = "0.0500"
	writeReadinessEvidenceArtifact(t, manifestDir, "daily.json", &artifactMetrics)

	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "daily.json", EvidenceMetrics: manifestMetrics},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	require.NoError(t, guard(context.Background(), "chat-1", "tester"))
}

func TestManifestLiveModeGuardAllowsWrappedManifestEntryEvidenceMetrics(t *testing.T) {
	manifestDir := t.TempDir()
	metrics := passingReadinessEvidence(2)
	evidencePath := filepath.Join(manifestDir, "daily.json")
	rawEvidence, err := json.Marshal(map[string]interface{}{
		"live_readiness": map[string]interface{}{
			"manifest_entry": map[string]interface{}{
				"evidence_metrics": metrics,
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(evidencePath, rawEvidence, 0o600))

	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "daily.json", EvidenceMetrics: metrics},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	require.NoError(t, guard(context.Background(), "chat-1", "tester"))
}

func TestManifestLiveModeGuardRejectsOversizedEvidenceArtifact(t *testing.T) {
	manifestDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(manifestDir, "daily.json"),
		[]byte(strings.Repeat(" ", maximumReadinessEvidenceBytes+1)),
		0o600,
	))
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "daily.json", EvidenceMetrics: passingReadinessEvidence(2)},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `daily_trading=evidence_too_large_"daily.json"`)
}

func TestManifestLiveModeGuardRequiresPaperTradingProofMetrics(t *testing.T) {
	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-readiness.json")
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"paper_trading": {
				Ready:    true,
				Evidence: "paper-readiness.json",
				EvidenceMetrics: &StrategyReadinessEvidence{
					ClosedTrades:   0,
					OpenPositions:  1,
					NetPnL:         "0",
					AvgNetPnL:      "-0.01",
					DiagnosticOnly: true,
				},
			},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"paper_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "paper_trading=runtime_probe_not_passed")
	assert.Contains(t, err.Error(), "paper_trading=lifecycle_storage_not_verified")
	assert.Contains(t, err.Error(), "paper_trading=insufficient_closed_trades")
	assert.Contains(t, err.Error(), "paper_trading=insufficient_validation_window")
	assert.Contains(t, err.Error(), "paper_trading=insufficient_strategy_coverage")
	assert.Contains(t, err.Error(), "paper_trading=risk_limits_not_enforced")
	assert.Contains(t, err.Error(), "paper_trading=backtest_comparison_not_verified")
	assert.Contains(t, err.Error(), "paper_trading=open_positions_1")
	assert.Contains(t, err.Error(), "paper_trading=non_positive_net_pnl")
	assert.Contains(t, err.Error(), "paper_trading=non_positive_avg_net_pnl")
	assert.Contains(t, err.Error(), "paper_trading=diagnostic_only")
}

func TestManifestLiveModeGuardRequiresPaperTradingAcceptanceMetrics(t *testing.T) {
	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-readiness.json")
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"paper_trading": {
				Ready:    true,
				Evidence: "paper-readiness.json",
				EvidenceMetrics: &StrategyReadinessEvidence{
					ClosedTrades:             1,
					OpenPositions:            0,
					NetPnL:                   "1.25",
					AvgNetPnL:                "1.25",
					PaperRuntimeProbePassed:  true,
					LifecycleStorageVerified: true,
				},
			},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"paper_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "paper_trading=insufficient_validation_window")
	assert.Contains(t, err.Error(), "paper_trading=insufficient_strategy_coverage")
	assert.Contains(t, err.Error(), "paper_trading=risk_limits_not_enforced")
	assert.Contains(t, err.Error(), "paper_trading=backtest_comparison_not_verified")
	assert.NotContains(t, err.Error(), "paper_trading=runtime_probe_not_passed")
	assert.NotContains(t, err.Error(), "paper_trading=lifecycle_storage_not_verified")
	assert.NotContains(t, err.Error(), "paper_trading=insufficient_closed_trades")
}

func TestManifestLiveModeGuardRequiresTradingProofMetrics(t *testing.T) {
	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-soak/swing.json")
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"swing_trading": {
				Ready:    true,
				Evidence: "paper-soak/swing.json",
				EvidenceMetrics: &StrategyReadinessEvidence{
					ClosedTrades:   1,
					WinningTrades:  1,
					LosingTrades:   1,
					OpenPositions:  1,
					NetPnL:         "-1.25",
					AvgNetPnL:      "0",
					MaxDrawdownPct: "0",
					DiagnosticOnly: true,
				},
			},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"swing_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "swing_trading=insufficient_closed_trades")
	assert.Contains(t, err.Error(), "swing_trading=execution_path_not_verified")
	assert.Contains(t, err.Error(), "swing_trading=market_data_not_verified")
	assert.Contains(t, err.Error(), "swing_trading=risk_limits_not_enforced")
	assert.Contains(t, err.Error(), "swing_trading=backtest_comparison_not_verified")
	assert.Contains(t, err.Error(), "swing_trading=hold_window_not_verified")
	assert.Contains(t, err.Error(), "swing_trading=inconsistent_trade_counts")
	assert.Contains(t, err.Error(), "swing_trading=open_positions_1")
	assert.Contains(t, err.Error(), "swing_trading=non_positive_net_pnl")
	assert.Contains(t, err.Error(), "swing_trading=non_positive_avg_net_pnl")
	assert.Contains(t, err.Error(), "swing_trading=missing_observed_drawdown")
	assert.Contains(t, err.Error(), "swing_trading=drawdown_not_verified")
	assert.Contains(t, err.Error(), "swing_trading=diagnostic_only")
}

func TestManifestLiveModeGuardRequiresStrategyAcceptanceMetrics(t *testing.T) {
	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-soak/daily.json")
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {
				Ready:    true,
				Evidence: "paper-soak/daily.json",
				EvidenceMetrics: &StrategyReadinessEvidence{
					ClosedTrades:     2,
					WinningTrades:    1,
					LosingTrades:     1,
					NetPnL:           "1.25",
					AvgNetPnL:        "0.10",
					MaxDrawdownPct:   "0.05",
					DrawdownVerified: true,
				},
			},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daily_trading=execution_path_not_verified")
	assert.Contains(t, err.Error(), "daily_trading=market_data_not_verified")
	assert.Contains(t, err.Error(), "daily_trading=risk_limits_not_enforced")
	assert.Contains(t, err.Error(), "daily_trading=backtest_comparison_not_verified")
	assert.NotContains(t, err.Error(), "daily_trading=insufficient_closed_trades")
	assert.NotContains(t, err.Error(), "daily_trading=drawdown_not_verified")
}

func TestManifestLiveModeGuardRejectsUnverifiedDrawdownEvenWithPositiveMetric(t *testing.T) {
	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-soak/daily.json")
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"daily_trading": {Ready: true, Evidence: "paper-soak/daily.json", EvidenceMetrics: &StrategyReadinessEvidence{
				ClosedTrades:   2,
				WinningTrades:  1,
				LosingTrades:   1,
				NetPnL:         "1.25",
				AvgNetPnL:      "0.10",
				MaxDrawdownPct: "0.05",
			}},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"daily_trading"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daily_trading=drawdown_not_verified")
}

func TestManifestLiveModeGuardRequiresScalpingMinimumTradeProof(t *testing.T) {
	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-soak/scalping.json")
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"scalping": {Ready: true, Evidence: "paper-soak/scalping.json", EvidenceMetrics: passingReadinessEvidence(19)},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"scalping"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scalping=insufficient_closed_trades")
}

func TestManifestLiveModeGuardAllowsArbitrageNoTradeSafetyEvidence(t *testing.T) {
	manifestDir := t.TempDir()
	metrics := &StrategyReadinessEvidence{
		NoTradeSafety:          true,
		NoTradeReason:          "no executable spreads after fees across observed window",
		MarketDataVerified:     true,
		CostAccountingVerified: true,
		ExposureSafetyVerified: true,
	}
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-safety/arbitrage.json", metrics)
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"arbitrage": {
				Ready:           true,
				Evidence:        "paper-safety/arbitrage.json",
				EvidenceMetrics: metrics,
			},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"arbitrage"})
	require.NoError(t, guard(context.Background(), "chat-1", "tester"))
}

func TestManifestLiveModeGuardQuotesArbitrageNoTradeSafetyBlockers(t *testing.T) {
	manifestDir := t.TempDir()
	writeReadinessEvidenceArtifact(t, manifestDir, "paper-safety/arbitrage.json")
	manifestPath := filepath.Join(manifestDir, "live-readiness.json")
	manifest := LiveReadinessManifest{
		Strategies: map[string]StrategyLiveReadiness{
			"arbitrage": {
				Ready:    true,
				Evidence: "paper-safety/arbitrage.json",
				EvidenceMetrics: &StrategyReadinessEvidence{
					NoTradeSafety: true,
					OpenPositions: 2,
				},
			},
		},
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))

	guard := ManifestLiveModeGuard(manifestPath, []string{"arbitrage"})
	err = guard(context.Background(), "chat-1", "tester")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `arbitrage="missing_no_trade_reason"`)
	assert.Contains(t, err.Error(), `arbitrage=open_positions_"2"`)
	assert.Contains(t, err.Error(), "arbitrage=market_data_not_verified")
	assert.Contains(t, err.Error(), "arbitrage=cost_accounting_not_verified")
	assert.Contains(t, err.Error(), "arbitrage=exposure_safety_not_verified")
}

func writeReadinessEvidenceArtifact(t *testing.T, manifestDir string, evidence string, metrics ...*StrategyReadinessEvidence) {
	t.Helper()
	path := strings.TrimSpace(evidence)
	if !filepath.IsAbs(path) {
		path = filepath.Join(manifestDir, path)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	payload := map[string]interface{}{"verified": true}
	if len(metrics) > 0 && metrics[0] != nil {
		payload["evidence_metrics"] = metrics[0]
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o600))
}

func passingReadinessEvidence(closedTrades int) *StrategyReadinessEvidence {
	return &StrategyReadinessEvidence{
		ClosedTrades:               closedTrades,
		WinningTrades:              1,
		LosingTrades:               1,
		NetPnL:                     "1.25",
		AvgNetPnL:                  "0.10",
		MaxDrawdownPct:             "0.05",
		DrawdownVerified:           true,
		ExecutionPathVerified:      true,
		MarketDataVerified:         true,
		RiskLimitsEnforced:         true,
		BacktestComparisonVerified: true,
		HoldWindowVerified:         true,
		CostAccountingVerified:     true,
		ExposureSafetyVerified:     true,
	}
}

func passingPaperReadinessEvidence() *StrategyReadinessEvidence {
	return &StrategyReadinessEvidence{
		ClosedTrades:               1,
		OpenPositions:              0,
		NetPnL:                     "1.25",
		AvgNetPnL:                  "1.25",
		PaperRuntimeProbePassed:    true,
		LifecycleStorageVerified:   true,
		ContinuousValidationHours:  minimumPaperTradingValidationHours,
		StrategyCount:              minimumPaperTradingStrategyCount,
		RiskLimitsEnforced:         true,
		BacktestComparisonVerified: true,
	}
}

func TestRuntimeModeOverrideFromEnv_HonorsSingularAndPluralAliases(t *testing.T) {
	testCases := []struct {
		name     string
		env      map[string]string
		expected OperationalMode
		ok       bool
	}{
		{
			name: "singular paper enabled and real disabled",
			env: map[string]string{
				envFeaturePaperTrading: "true",
				envFeatureRealTrading:  "false",
			},
			expected: ModePaper,
			ok:       true,
		},
		{
			name: "plural paper enabled and real disabled",
			env: map[string]string{
				envFeaturesPaperTrading: "true",
				envFeaturesRealTrading:  "false",
			},
			expected: ModePaper,
			ok:       true,
		},
		{
			name: "real disabled without paper falls back to dry",
			env: map[string]string{
				envFeatureRealTrading: "false",
			},
			expected: OpModeDry,
			ok:       true,
		},
		{
			name: "invalid real trading value is treated as disabled",
			env: map[string]string{
				envFeatureRealTrading: "flase",
			},
			expected: OpModeDry,
			ok:       true,
		},
		{
			name: "paper aliases prefer non-live when conflicting",
			env: map[string]string{
				envFeaturesPaperTrading: "false",
				envFeaturePaperTrading:  "true",
			},
			expected: ModePaper,
			ok:       true,
		},
		{
			name: "real aliases prefer disabled when conflicting",
			env: map[string]string{
				envFeaturesRealTrading: "true",
				envFeatureRealTrading:  "false",
			},
			expected: OpModeDry,
			ok:       true,
		},
		{
			name: "paper and real both enabled does not override persisted state",
			env: map[string]string{
				envFeaturePaperTrading: "true",
				envFeatureRealTrading:  "true",
			},
			ok: false,
		},
		{
			name: "plural paper and real both enabled does not override persisted state",
			env: map[string]string{
				envFeaturesPaperTrading: "true",
				envFeaturesRealTrading:  "true",
			},
			ok: false,
		},
		{
			name: "mixed paper and real aliases enabled does not override persisted state",
			env: map[string]string{
				envFeaturesPaperTrading: "true",
				envFeatureRealTrading:   "true",
			},
			ok: false,
		},
		{
			name: "unset env does not override persisted state",
			env:  map[string]string{},
			ok:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			unsetRuntimeModeEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			mode, ok := runtimeModeOverrideFromEnv()
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.expected, mode)
		})
	}
}

func unsetRuntimeModeEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{envFeaturePaperTrading, envFeaturesPaperTrading, envFeatureRealTrading, envFeaturesRealTrading} {
		previous, ok := os.LookupEnv(key)
		require.NoError(t, os.Unsetenv(key))
		t.Cleanup(func() {
			if ok {
				require.NoError(t, os.Setenv(key, previous))
				return
			}
			require.NoError(t, os.Unsetenv(key))
		})
	}
}
