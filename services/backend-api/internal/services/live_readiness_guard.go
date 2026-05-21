package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	// LiveReadinessManifestEnv is the path to the manifest that records which
	// strategies have verified evidence before global live mode can be enabled.
	LiveReadinessManifestEnv = "NEURATRADE_LIVE_READINESS_MANIFEST"

	minimumPaperTradingValidationHours = 168
	minimumPaperTradingStrategyCount   = 2
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
	// ContinuousValidationHours, StrategyCount, RiskLimitsEnforced, and
	// BacktestComparisonVerified are paper_trading acceptance proof fields.
	ContinuousValidationHours  int  `json:"continuous_validation_hours,omitempty"`
	StrategyCount              int  `json:"strategy_count,omitempty"`
	RiskLimitsEnforced         bool `json:"risk_limits_enforced,omitempty"`
	BacktestComparisonVerified bool `json:"backtest_comparison_verified,omitempty"`
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
				blockers = append(blockers, strategyReadinessEvidenceBlockers(strategy, status)...)
			}
		}
		if len(blockers) > 0 {
			return fmt.Errorf("live mode blocked: readiness proof incomplete (%s)", strings.Join(blockers, "; "))
		}

		return nil
	}
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
		return blockers
	}

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
		blockers = append(blockers, "paper_trading=backtest_comparison_missing")
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
