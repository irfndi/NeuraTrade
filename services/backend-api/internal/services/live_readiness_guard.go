package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	// LiveReadinessManifestEnv is the path to the manifest that records which
	// strategies have verified evidence before global live mode can be enabled.
	LiveReadinessManifestEnv = "NEURATRADE_LIVE_READINESS_MANIFEST"

	minimumPaperTradingValidationHours = 168
	minimumPaperTradingStrategyCount   = 2
	maximumReadinessEvidenceBytes      = 1024 * 1024
)

// LiveModeGuard blocks or permits a transition into real-money live mode.
type LiveModeGuard func(ctx context.Context, chatID string, changedBy string) error

// StrategyLiveReadiness records one strategy's live-readiness evidence.
type StrategyLiveReadiness struct {
	Ready           bool                       `json:"ready"`
	Evidence        string                     `json:"evidence,omitempty"`
	EvidenceMetrics *StrategyReadinessEvidence `json:"evidence_metrics,omitempty"`
	Reason          string                     `json:"reason,omitempty"`
	VerifiedAt      string                     `json:"verified_at,omitempty"`
}

// StrategyReadinessEvidence records the minimum proof needed before a trading
// strategy may be represented as live-ready in the operator manifest.
type StrategyReadinessEvidence struct {
	ClosedTrades     int    `json:"closed_trades,omitempty"`
	WinningTrades    int    `json:"winning_trades,omitempty"`
	LosingTrades     int    `json:"losing_trades,omitempty"`
	OpenPositions    int    `json:"open_positions,omitempty"`
	NetPnL           string `json:"net_pnl,omitempty"`
	AvgNetPnL        string `json:"avg_net_pnl,omitempty"`
	MaxDrawdownPct   string `json:"max_drawdown_pct,omitempty"`
	DrawdownVerified bool   `json:"drawdown_verified,omitempty"`
	DiagnosticOnly   bool   `json:"diagnostic_only,omitempty"`
	NoTradeSafety    bool   `json:"no_trade_safety,omitempty"`
	NoTradeReason    string `json:"no_trade_reason,omitempty"`
	// PaperRuntimeProbePassed and LifecycleStorageVerified are required only for
	// paper_trading evidence, where simulator and persistence proof are the base
	// readiness contract rather than strategy profitability.
	PaperRuntimeProbePassed  bool `json:"paper_runtime_probe_passed,omitempty"`
	LifecycleStorageVerified bool `json:"lifecycle_storage_verified,omitempty"`
	// Remaining fields are explicit acceptance proof fields used by the live
	// readiness guard so profitability metrics alone cannot approve real money.
	ContinuousValidationHours  int  `json:"continuous_validation_hours,omitempty"`
	StrategyCount              int  `json:"strategy_count,omitempty"`
	ExecutionPathVerified      bool `json:"execution_path_verified,omitempty"`
	MarketDataVerified         bool `json:"market_data_verified,omitempty"`
	RiskLimitsEnforced         bool `json:"risk_limits_enforced,omitempty"`
	BacktestComparisonVerified bool `json:"backtest_comparison_verified,omitempty"`
	HoldWindowVerified         bool `json:"hold_window_verified,omitempty"`
	CostAccountingVerified     bool `json:"cost_accounting_verified,omitempty"`
	ExposureSafetyVerified     bool `json:"exposure_safety_verified,omitempty"`
}

// LiveReadinessManifest is the file format consumed by ManifestLiveModeGuard.
type LiveReadinessManifest struct {
	UpdatedAt  string                           `json:"updated_at,omitempty"`
	Strategies map[string]StrategyLiveReadiness `json:"strategies"`
}

// DefaultLiveReadinessStrategies returns every strategy/operator surface that
// must have proof before the global live mode can be used for real money.
func DefaultLiveReadinessStrategies() []string {
	return []string{"paper_trading", "scalping", "daily_trading", "swing_trading", "arbitrage"}
}

// ManifestLiveModeGuard requires a JSON manifest with ready=true and non-empty
// evidence for each required strategy.
func ManifestLiveModeGuard(manifestPath string, requiredStrategies []string) LiveModeGuard {
	required := normalizeReadinessStrategies(requiredStrategies)
	return func(_ context.Context, _ string, _ string) error {
		if len(required) == 0 {
			return nil
		}
		path := strings.TrimSpace(manifestPath)
		if path == "" {
			return fmt.Errorf(
				"live mode blocked: %s is required with verified evidence for %s",
				LiveReadinessManifestEnv,
				strings.Join(required, ", "),
			)
		}

		// #nosec G304 -- path is operator-configured via NEURATRADE_LIVE_READINESS_MANIFEST.
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("live mode blocked: read readiness manifest %q: %w", path, err)
		}

		var manifest LiveReadinessManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return fmt.Errorf("live mode blocked: parse readiness manifest %q: %w", path, err)
		}

		strategies := normalizeReadinessManifestStrategies(manifest.Strategies)
		manifestDir := filepath.Dir(path)
		var blockers []string
		for _, strategy := range required {
			status, ok := strategies[strategy]
			switch {
			case !ok:
				blockers = append(blockers, fmt.Sprintf("%s=missing", strategy))
			case !status.Ready:
				reason := strings.TrimSpace(status.Reason)
				if reason == "" {
					reason = "not_ready"
				}
				blockers = append(blockers, fmt.Sprintf("%s=%s", strategy, reason))
			case strings.TrimSpace(status.Evidence) == "":
				blockers = append(blockers, fmt.Sprintf("%s=missing_evidence", strategy))
			default:
				blockers = append(blockers, evidenceArtifactBlockers(strategy, status.Evidence, manifestDir, status.EvidenceMetrics, status.VerifiedAt)...)
				blockers = append(blockers, readinessVerifiedAtBlockers(strategy, status)...)
				blockers = append(blockers, strategyReadinessEvidenceBlockers(strategy, status)...)
			}
		}
		if len(blockers) > 0 {
			return fmt.Errorf("live mode blocked: readiness proof incomplete (%s)", strings.Join(blockers, "; "))
		}

		return nil
	}
}

func evidenceArtifactBlockers(
	strategy string,
	evidence string,
	manifestDir string,
	expectedMetrics *StrategyReadinessEvidence,
	expectedVerifiedAt string,
) []string {
	evidence = strings.TrimSpace(evidence)
	evidencePath := evidence
	if !filepath.IsAbs(evidencePath) {
		evidencePath = filepath.Join(manifestDir, evidencePath)
	}

	info, err := os.Stat(evidencePath)
	if err != nil {
		return []string{fmt.Sprintf("%s=evidence_unreadable_%q", strategy, evidence)}
	}
	if info.IsDir() {
		return []string{fmt.Sprintf("%s=evidence_not_file_%q", strategy, evidence)}
	}
	if info.Size() == 0 {
		return []string{fmt.Sprintf("%s=evidence_empty_%q", strategy, evidence)}
	}
	if info.Size() > maximumReadinessEvidenceBytes {
		return []string{fmt.Sprintf("%s=evidence_too_large_%q", strategy, evidence)}
	}

	raw, err := os.ReadFile(evidencePath)
	if err != nil {
		return []string{fmt.Sprintf("%s=evidence_unreadable_%q", strategy, evidence)}
	}
	if !json.Valid(raw) {
		return []string{fmt.Sprintf("%s=evidence_invalid_json_%q", strategy, evidence)}
	}
	var evidenceObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &evidenceObject); err != nil || evidenceObject == nil {
		return []string{fmt.Sprintf("%s=evidence_not_json_object_%q", strategy, evidence)}
	}
	if expectedMetrics != nil {
		if blockers := evidenceArtifactMetricsBlockers(strategy, evidence, evidenceObject, expectedMetrics); len(blockers) > 0 {
			return blockers
		}
	}
	if strings.TrimSpace(expectedVerifiedAt) != "" {
		if blockers := evidenceArtifactVerifiedAtBlockers(strategy, evidence, evidenceObject, expectedVerifiedAt); len(blockers) > 0 {
			return blockers
		}
	}
	return evidenceArtifactStrategyBlockers(strategy, evidence, evidenceObject)
}

func readinessVerifiedAtBlockers(strategy string, status StrategyLiveReadiness) []string {
	verifiedAt := strings.TrimSpace(status.VerifiedAt)
	if verifiedAt == "" {
		return []string{fmt.Sprintf("%s=missing_verified_at", strategy)}
	}
	if _, err := time.Parse(time.RFC3339Nano, verifiedAt); err != nil {
		return []string{fmt.Sprintf("%s=invalid_verified_at_%q", strategy, verifiedAt)}
	}
	return nil
}

func evidenceArtifactMetricsBlockers(
	strategy string,
	evidence string,
	evidenceObject map[string]json.RawMessage,
	expectedMetrics *StrategyReadinessEvidence,
) []string {
	actualMetrics, ok := evidenceArtifactMetrics(evidenceObject)
	if !ok {
		return []string{fmt.Sprintf("%s=evidence_missing_metrics_%q", strategy, evidence)}
	}
	if !readinessEvidenceMatches(*expectedMetrics, actualMetrics) {
		return []string{fmt.Sprintf("%s=evidence_metrics_mismatch_%q", strategy, evidence)}
	}
	return nil
}

func readinessEvidenceMatches(expected StrategyReadinessEvidence, actual StrategyReadinessEvidence) bool {
	if !matchingDecimalString(expected.NetPnL, actual.NetPnL) {
		return false
	}
	if !matchingDecimalString(expected.AvgNetPnL, actual.AvgNetPnL) {
		return false
	}
	if !matchingDecimalString(expected.MaxDrawdownPct, actual.MaxDrawdownPct) {
		return false
	}
	expected.NetPnL = ""
	actual.NetPnL = ""
	expected.AvgNetPnL = ""
	actual.AvgNetPnL = ""
	expected.MaxDrawdownPct = ""
	actual.MaxDrawdownPct = ""
	return expected == actual
}

func matchingDecimalString(expected string, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" || actual == "" {
		return expected == actual
	}
	expectedDecimal, expectedErr := decimal.NewFromString(expected)
	actualDecimal, actualErr := decimal.NewFromString(actual)
	if expectedErr != nil || actualErr != nil {
		return expected == actual
	}
	return expectedDecimal.Equal(actualDecimal)
}

func evidenceArtifactMetrics(evidenceObject map[string]json.RawMessage) (StrategyReadinessEvidence, bool) {
	if raw, ok := evidenceObject["evidence_metrics"]; ok {
		var metrics StrategyReadinessEvidence
		if err := json.Unmarshal(raw, &metrics); err == nil {
			return metrics, true
		}
		return StrategyReadinessEvidence{}, false
	}

	var readiness struct {
		ManifestEntry struct {
			EvidenceMetrics *StrategyReadinessEvidence `json:"evidence_metrics"`
		} `json:"manifest_entry"`
	}
	raw, ok := evidenceObject["live_readiness"]
	if !ok {
		return StrategyReadinessEvidence{}, false
	}
	if err := json.Unmarshal(raw, &readiness); err != nil || readiness.ManifestEntry.EvidenceMetrics == nil {
		return StrategyReadinessEvidence{}, false
	}
	return *readiness.ManifestEntry.EvidenceMetrics, true
}

func evidenceArtifactVerifiedAtBlockers(
	strategy string,
	evidence string,
	evidenceObject map[string]json.RawMessage,
	expectedVerifiedAt string,
) []string {
	actualVerifiedAt, ok := evidenceArtifactVerifiedAt(evidenceObject)
	if !ok {
		return []string{fmt.Sprintf("%s=evidence_missing_verified_at_%q", strategy, evidence)}
	}
	expectedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(expectedVerifiedAt))
	if err != nil {
		return nil
	}
	actualAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(actualVerifiedAt))
	if err != nil {
		return []string{fmt.Sprintf("%s=evidence_invalid_verified_at_%q", strategy, actualVerifiedAt)}
	}
	if !expectedAt.Equal(actualAt) {
		return []string{fmt.Sprintf("%s=evidence_verified_at_mismatch_%q", strategy, evidence)}
	}
	return nil
}

func evidenceArtifactVerifiedAt(evidenceObject map[string]json.RawMessage) (string, bool) {
	if raw, ok := evidenceObject["verified_at"]; ok {
		var verifiedAt string
		if err := json.Unmarshal(raw, &verifiedAt); err != nil {
			return "", false
		}
		verifiedAt = strings.TrimSpace(verifiedAt)
		return verifiedAt, verifiedAt != ""
	}

	var readiness struct {
		ManifestEntry struct {
			VerifiedAt string `json:"verified_at"`
		} `json:"manifest_entry"`
	}
	raw, ok := evidenceObject["live_readiness"]
	if !ok {
		return "", false
	}
	if err := json.Unmarshal(raw, &readiness); err != nil {
		return "", false
	}
	verifiedAt := strings.TrimSpace(readiness.ManifestEntry.VerifiedAt)
	return verifiedAt, verifiedAt != ""
}

func evidenceArtifactStrategyBlockers(
	strategy string,
	evidence string,
	evidenceObject map[string]json.RawMessage,
) []string {
	actualStrategy, ok := evidenceArtifactStrategy(evidenceObject)
	if !ok {
		return []string{fmt.Sprintf("%s=evidence_missing_strategy_%q", strategy, evidence)}
	}
	actualStrategy = strings.ToLower(strings.TrimSpace(actualStrategy))
	if actualStrategy != strategy {
		return []string{fmt.Sprintf("%s=evidence_strategy_mismatch_%q_%q", strategy, actualStrategy, evidence)}
	}
	return nil
}

func evidenceArtifactStrategy(evidenceObject map[string]json.RawMessage) (string, bool) {
	if raw, ok := evidenceObject["strategy"]; ok {
		var strategy string
		if err := json.Unmarshal(raw, &strategy); err != nil {
			return "", false
		}
		strategy = strings.TrimSpace(strategy)
		return strategy, strategy != ""
	}

	var readiness struct {
		Strategy string `json:"strategy"`
	}
	raw, ok := evidenceObject["live_readiness"]
	if !ok {
		return "", false
	}
	if err := json.Unmarshal(raw, &readiness); err != nil {
		return "", false
	}
	strategy := strings.TrimSpace(readiness.Strategy)
	return strategy, strategy != ""
}

func normalizeReadinessStrategies(strategies []string) []string {
	seen := make(map[string]struct{}, len(strategies))
	normalized := make([]string, 0, len(strategies))
	for _, strategy := range strategies {
		strategy = strings.ToLower(strings.TrimSpace(strategy))
		if strategy == "" {
			continue
		}
		if _, ok := seen[strategy]; ok {
			continue
		}
		seen[strategy] = struct{}{}
		normalized = append(normalized, strategy)
	}
	sort.Strings(normalized)
	return normalized
}

func strategyReadinessEvidenceBlockers(strategy string, status StrategyLiveReadiness) []string {
	metrics := status.EvidenceMetrics
	if metrics == nil {
		return []string{fmt.Sprintf("%s=missing_evidence_metrics", strategy)}
	}
	var blockers []string
	if metrics.DiagnosticOnly {
		blockers = append(blockers, fmt.Sprintf("%s=diagnostic_only", strategy))
	}
	if strategy == "paper_trading" {
		return append(blockers, paperTradingReadinessEvidenceBlockers(metrics)...)
	}
	if strategy == "arbitrage" && metrics.NoTradeSafety {
		if strings.TrimSpace(metrics.NoTradeReason) == "" {
			blockers = append(blockers, fmt.Sprintf("%s=%q", strategy, "missing_no_trade_reason"))
		}
		if metrics.OpenPositions != 0 {
			blockers = append(blockers, fmt.Sprintf("%s=open_positions_%q", strategy, fmt.Sprint(metrics.OpenPositions)))
		}
		if !metrics.MarketDataVerified {
			blockers = append(blockers, fmt.Sprintf("%s=market_data_not_verified", strategy))
		}
		blockers = append(blockers, arbitrageSafetyEvidenceBlockers(strategy, metrics)...)
		return blockers
	}

	blockers = append(blockers, strategyAcceptanceEvidenceBlockers(strategy, metrics)...)
	if metrics.ClosedTrades < minimumReadinessClosedTrades(strategy) {
		blockers = append(blockers, fmt.Sprintf("%s=insufficient_closed_trades", strategy))
	}
	if metrics.ClosedTrades < metrics.WinningTrades+metrics.LosingTrades {
		blockers = append(blockers, fmt.Sprintf("%s=inconsistent_trade_counts", strategy))
	}
	if metrics.WinningTrades <= 0 {
		blockers = append(blockers, fmt.Sprintf("%s=no_winning_trades", strategy))
	}
	if metrics.LosingTrades <= 0 {
		blockers = append(blockers, fmt.Sprintf("%s=no_losing_trades", strategy))
	}
	if metrics.OpenPositions != 0 {
		blockers = append(blockers, fmt.Sprintf("%s=open_positions_%d", strategy, metrics.OpenPositions))
	}
	if !positiveDecimalString(metrics.NetPnL) {
		blockers = append(blockers, fmt.Sprintf("%s=non_positive_net_pnl", strategy))
	}
	if !positiveDecimalString(metrics.AvgNetPnL) {
		blockers = append(blockers, fmt.Sprintf("%s=non_positive_avg_net_pnl", strategy))
	}
	if !positiveDecimalString(metrics.MaxDrawdownPct) {
		blockers = append(blockers, fmt.Sprintf("%s=missing_observed_drawdown", strategy))
	}
	if !metrics.DrawdownVerified {
		blockers = append(blockers, fmt.Sprintf("%s=drawdown_not_verified", strategy))
	}
	return blockers
}

func strategyAcceptanceEvidenceBlockers(strategy string, metrics *StrategyReadinessEvidence) []string {
	var blockers []string
	if !metrics.ExecutionPathVerified {
		blockers = append(blockers, fmt.Sprintf("%s=execution_path_not_verified", strategy))
	}
	if !metrics.MarketDataVerified {
		blockers = append(blockers, fmt.Sprintf("%s=market_data_not_verified", strategy))
	}
	if !metrics.RiskLimitsEnforced {
		blockers = append(blockers, fmt.Sprintf("%s=risk_limits_not_enforced", strategy))
	}
	if !metrics.BacktestComparisonVerified {
		blockers = append(blockers, fmt.Sprintf("%s=backtest_comparison_not_verified", strategy))
	}
	if strategy == "swing_trading" && !metrics.HoldWindowVerified {
		blockers = append(blockers, "swing_trading=hold_window_not_verified")
	}
	if strategy == "arbitrage" {
		blockers = append(blockers, arbitrageSafetyEvidenceBlockers(strategy, metrics)...)
	}
	return blockers
}

func arbitrageSafetyEvidenceBlockers(strategy string, metrics *StrategyReadinessEvidence) []string {
	var blockers []string
	if !metrics.CostAccountingVerified {
		blockers = append(blockers, fmt.Sprintf("%s=cost_accounting_not_verified", strategy))
	}
	if !metrics.ExposureSafetyVerified {
		blockers = append(blockers, fmt.Sprintf("%s=exposure_safety_not_verified", strategy))
	}
	return blockers
}

func paperTradingReadinessEvidenceBlockers(metrics *StrategyReadinessEvidence) []string {
	var blockers []string
	if !metrics.PaperRuntimeProbePassed {
		blockers = append(blockers, "paper_trading=runtime_probe_not_passed")
	}
	if !metrics.LifecycleStorageVerified {
		blockers = append(blockers, "paper_trading=lifecycle_storage_not_verified")
	}
	if metrics.ClosedTrades < 1 {
		blockers = append(blockers, "paper_trading=insufficient_closed_trades")
	}
	if metrics.ContinuousValidationHours < minimumPaperTradingValidationHours {
		blockers = append(blockers, "paper_trading=insufficient_validation_window")
	}
	if metrics.StrategyCount < minimumPaperTradingStrategyCount {
		blockers = append(blockers, "paper_trading=insufficient_strategy_coverage")
	}
	if !metrics.RiskLimitsEnforced {
		blockers = append(blockers, "paper_trading=risk_limits_not_enforced")
	}
	if !metrics.BacktestComparisonVerified {
		blockers = append(blockers, "paper_trading=backtest_comparison_not_verified")
	}
	if metrics.OpenPositions != 0 {
		blockers = append(blockers, fmt.Sprintf("paper_trading=open_positions_%d", metrics.OpenPositions))
	}
	if !positiveDecimalString(metrics.NetPnL) {
		blockers = append(blockers, "paper_trading=non_positive_net_pnl")
	}
	if !positiveDecimalString(metrics.AvgNetPnL) {
		blockers = append(blockers, "paper_trading=non_positive_avg_net_pnl")
	}
	return blockers
}

func minimumReadinessClosedTrades(strategy string) int {
	switch strategy {
	case "scalping":
		return 20
	default:
		return 2
	}
}

func positiveDecimalString(raw string) bool {
	value, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return value.GreaterThan(decimal.Zero)
}

func normalizeReadinessManifestStrategies(strategies map[string]StrategyLiveReadiness) map[string]StrategyLiveReadiness {
	normalized := make(map[string]StrategyLiveReadiness, len(strategies))
	for strategy, status := range strategies {
		key := strings.ToLower(strings.TrimSpace(strategy))
		if key == "" {
			continue
		}
		normalized[key] = status
	}
	return normalized
}
