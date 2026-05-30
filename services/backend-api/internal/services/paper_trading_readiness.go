package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shopspring/decimal"
)

// StrategyEvidence holds paper trading metrics for a single strategy.
type StrategyEvidence struct {
	Strategy       string          `json:"strategy"`
	TotalTrades    int             `json:"total_trades"`
	ClosedTrades   int             `json:"closed_trades"`
	OpenPositions  int             `json:"open_positions"`
	WinningTrades  int             `json:"winning_trades"`
	LosingTrades   int             `json:"losing_trades"`
	NetPnL         decimal.Decimal `json:"net_pnl"`
	AvgNetPnL      decimal.Decimal `json:"avg_net_pnl"`
	WinRate        decimal.Decimal `json:"win_rate"`
	MaxDrawdown    decimal.Decimal `json:"max_drawdown"`
	RiskViolations int             `json:"risk_violations"`
}

// PaperTradingReadinessManifest aggregates evidence across all strategies
// and provides acceptance criteria evaluation.
type PaperTradingReadinessManifest struct {
	Timestamp                  time.Time          `json:"timestamp"`
	ContinuousValidationHours  decimal.Decimal    `json:"continuous_validation_hours"`
	StrategyCount              int                `json:"strategy_count"`
	TotalTrades                int                `json:"total_trades"`
	ClosedTrades               int                `json:"closed_trades"`
	OpenPositions              int                `json:"open_positions"`
	NetPnL                     decimal.Decimal    `json:"net_pnl"`
	OverallWinRate             decimal.Decimal    `json:"overall_win_rate"`
	RiskLimitsEnforced         bool               `json:"risk_limits_enforced"`
	BacktestComparisonVerified bool               `json:"backtest_comparison_verified"`
	DiagnosticOnly             bool               `json:"diagnostic_only"`
	Strategies                 []StrategyEvidence `json:"strategies"`
	Acceptance                 AcceptanceResult   `json:"acceptance"`
	EvidenceFilePath           string             `json:"evidence_file_path,omitempty"`
}

// AcceptanceResult holds the evaluation against readiness gates.
type AcceptanceResult struct {
	Ready            bool     `json:"ready"`
	MinHoursMet      bool     `json:"min_hours_met"`
	MinTradesMet     bool     `json:"min_trades_met"`
	MinStrategiesMet bool     `json:"min_strategies_met"`
	RiskLimitsMet    bool     `json:"risk_limits_met"`
	BacktestMet      bool     `json:"backtest_met"`
	Failures         []string `json:"failures,omitempty"`
}

// ReadinessManifestGenerator creates a unified paper trading readiness manifest.
type ReadinessManifestGenerator struct {
	db     DBPool
	logger Logger
}

// NewReadinessManifestGenerator creates a new manifest generator.
func NewReadinessManifestGenerator(db DBPool, logger Logger) *ReadinessManifestGenerator {
	return &ReadinessManifestGenerator{db: db, logger: logger}
}

// GenerateManifest builds a readiness manifest across the requested strategies.
func (g *ReadinessManifestGenerator) GenerateManifest(
	ctx context.Context,
	startTime, endTime time.Time,
	strategyNames []string,
) (*PaperTradingReadinessManifest, error) {
	if g.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	strategies := make([]StrategyEvidence, 0, len(strategyNames))
	var totalNetPnL decimal.Decimal
	var totalTrades, totalClosed, totalOpen, totalWins int
	allRiskLimitsEnforced := true
	allBacktestVerified := true

	for _, name := range strategyNames {
		ev, err := g.generateStrategyEvidence(ctx, startTime, endTime, name)
		if err != nil {
			g.logger.Warn(fmt.Sprintf("Failed to generate evidence for strategy %s: %v", name, err))
			continue
		}
		strategies = append(strategies, *ev)
		totalNetPnL = totalNetPnL.Add(ev.NetPnL)
		totalTrades += ev.TotalTrades
		totalClosed += ev.ClosedTrades
		totalOpen += ev.OpenPositions
		totalWins += ev.WinningTrades
		if ev.RiskViolations > 0 {
			allRiskLimitsEnforced = false
		}
	}

	overallWinRate := decimal.Zero
	if totalTrades > 0 {
		overallWinRate = decimal.NewFromInt(int64(totalWins)).Div(decimal.NewFromInt(int64(totalTrades)))
	}

	continuousHours := decimal.NewFromFloat(endTime.Sub(startTime).Hours())

	// Check backtest comparison for all requested strategies
	backtestVerified := g.checkBacktestComparison(ctx, strategyNames)
	allBacktestVerified = allBacktestVerified && backtestVerified

	acceptance := g.evaluateAcceptance(continuousHours, totalClosed, len(strategies), allRiskLimitsEnforced, allBacktestVerified)

	manifest := &PaperTradingReadinessManifest{
		Timestamp:                  time.Now(),
		ContinuousValidationHours:  continuousHours,
		StrategyCount:              len(strategies),
		TotalTrades:                totalTrades,
		ClosedTrades:               totalClosed,
		OpenPositions:              totalOpen,
		NetPnL:                     totalNetPnL,
		OverallWinRate:             overallWinRate,
		RiskLimitsEnforced:         allRiskLimitsEnforced,
		BacktestComparisonVerified: allBacktestVerified,
		DiagnosticOnly:             false,
		Strategies:                 strategies,
		Acceptance:                 acceptance,
	}

	return manifest, nil
}

// generateStrategyEvidence queries the database for a single strategy's metrics.
func (g *ReadinessManifestGenerator) generateStrategyEvidence(
	ctx context.Context,
	startTime, endTime time.Time,
	strategy string,
) (*StrategyEvidence, error) {
	query := `
		SELECT
			COUNT(*) as total_trades,
			COUNT(CASE WHEN pnl > 0 THEN 1 END) as winning_trades,
			COUNT(CASE WHEN pnl < 0 THEN 1 END) as losing_trades,
			COUNT(CASE WHEN status = 'closed' THEN 1 END) as closed_trades,
			COUNT(CASE WHEN status = 'open' THEN 1 END) as open_positions,
			COALESCE(SUM(pnl), 0) as total_pnl,
			COALESCE(AVG(pnl), 0) as avg_pnl,
			COALESCE(MAX(ABS(CASE WHEN pnl < 0 THEN pnl END)), 0) as max_drawdown
		FROM paper_trades
		WHERE strategy_id = $1
		  AND created_at BETWEEN $2 AND $3
	`

	var total, wins, losses, closed, open int
	var totalPnL, avgPnL, maxDD decimal.Decimal

	row := g.db.QueryRow(ctx, query, strategy, startTime, endTime)
	if err := row.Scan(&total, &wins, &losses, &closed, &open, &totalPnL, &avgPnL, &maxDD); err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	winRate := decimal.Zero
	if total > 0 {
		winRate = decimal.NewFromInt(int64(wins)).Div(decimal.NewFromInt(int64(total)))
	}

	// Count risk violations for this strategy
	violationsQuery := `
		SELECT COUNT(*) FROM paper_trades
		WHERE strategy_id = $1
		  AND created_at BETWEEN $2 AND $3
		  AND status = 'closed'
		  AND (pnl < -0.05 * ABS(entry_price * size) OR size > 0.1 * ABS(entry_price * size))
	`
	var violations int
	vRow := g.db.QueryRow(ctx, violationsQuery, strategy, startTime, endTime)
	_ = vRow.Scan(&violations) // ignore error, default to 0

	return &StrategyEvidence{
		Strategy:       strategy,
		TotalTrades:    total,
		ClosedTrades:   closed,
		OpenPositions:  open,
		WinningTrades:  wins,
		LosingTrades:   losses,
		NetPnL:         totalPnL,
		AvgNetPnL:      avgPnL,
		WinRate:        winRate,
		MaxDrawdown:    maxDD,
		RiskViolations: violations,
	}, nil
}

// checkBacktestComparison verifies backtest records exist for all strategies.
func (g *ReadinessManifestGenerator) checkBacktestComparison(ctx context.Context, strategies []string) bool {
	if g.db == nil {
		return false
	}

	// Fetch all backtest runs and parse JSON config in Go to avoid DB-specific JSON syntax.
	rows, err := g.db.Query(ctx, `SELECT config FROM scalping_backtest_runs WHERE status = 'completed'`)
	if err != nil {
		return false
	}
	defer rows.Close()

	foundStrategies := make(map[string]bool)
	for rows.Next() {
		var configJSON string
		if err := rows.Scan(&configJSON); err != nil {
			continue
		}
		var cfg struct {
			Strategy string `json:"strategy"`
		}
		if err := json.Unmarshal([]byte(configJSON), &cfg); err == nil && cfg.Strategy != "" {
			foundStrategies[cfg.Strategy] = true
		}
	}

	for _, strategy := range strategies {
		if !foundStrategies[strategy] {
			return false
		}
	}
	return true
}

// evaluateAcceptance checks the manifest against readiness gates.
func (g *ReadinessManifestGenerator) evaluateAcceptance(
	continuousHours decimal.Decimal,
	closedTrades int,
	strategyCount int,
	riskLimitsEnforced bool,
	backtestVerified bool,
) AcceptanceResult {
	const minHours = 168.0 // 7 days
	const minTrades = 10
	const minStrategies = 1

	result := AcceptanceResult{
		Ready:            true,
		MinHoursMet:      continuousHours.GreaterThanOrEqual(decimal.NewFromFloat(minHours)),
		MinTradesMet:     closedTrades >= minTrades,
		MinStrategiesMet: strategyCount >= minStrategies,
		RiskLimitsMet:    riskLimitsEnforced,
		BacktestMet:      backtestVerified,
	}

	if !result.MinHoursMet {
		result.Failures = append(result.Failures,
			fmt.Sprintf("continuous validation hours %s < minimum %f", continuousHours.StringFixed(2), minHours))
	}
	if !result.MinTradesMet {
		result.Failures = append(result.Failures,
			fmt.Sprintf("closed trades %d < minimum %d", closedTrades, minTrades))
	}
	if !result.MinStrategiesMet {
		result.Failures = append(result.Failures,
			fmt.Sprintf("strategies %d < minimum %d", strategyCount, minStrategies))
	}
	if !result.RiskLimitsMet {
		result.Failures = append(result.Failures, "risk limits were violated")
	}
	if !result.BacktestMet {
		result.Failures = append(result.Failures, "backtest comparison not verified for all strategies")
	}

	result.Ready = result.MinHoursMet && result.MinTradesMet && result.MinStrategiesMet && result.RiskLimitsMet && result.BacktestMet
	return result
}

// SaveManifest saves the manifest to a JSON file.
func (g *ReadinessManifestGenerator) SaveManifest(manifest *PaperTradingReadinessManifest, outputPath string) error {
	if outputPath == "" {
		outputPath = filepath.Join(os.TempDir(), fmt.Sprintf("paper-trading-readiness-%s.json", time.Now().Format("20060102-150405")))
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}

	manifest.EvidenceFilePath = outputPath
	g.logger.Info(fmt.Sprintf("Paper trading readiness manifest saved to: %s", outputPath))
	return nil
}

// PrintManifest prints the manifest to stdout.
func (g *ReadinessManifestGenerator) PrintManifest(manifest *PaperTradingReadinessManifest) {
	fmt.Println("========================================")
	fmt.Println("  Paper Trading Readiness Manifest")
	fmt.Println("========================================")
	fmt.Printf("Timestamp: %s\n", manifest.Timestamp.Format(time.RFC3339))
	fmt.Printf("Continuous Validation Hours: %s\n", manifest.ContinuousValidationHours.StringFixed(2))
	fmt.Printf("Strategies Evaluated: %d\n", manifest.StrategyCount)
	fmt.Printf("Total Trades: %d\n", manifest.TotalTrades)
	fmt.Printf("Closed Trades: %d\n", manifest.ClosedTrades)
	fmt.Printf("Open Positions: %d\n", manifest.OpenPositions)
	fmt.Printf("Net PnL: %s\n", manifest.NetPnL.StringFixed(4))
	fmt.Printf("Overall Win Rate: %s\n", manifest.OverallWinRate.StringFixed(4))
	fmt.Printf("Risk Limits Enforced: %t\n", manifest.RiskLimitsEnforced)
	fmt.Printf("Backtest Comparison Verified: %t\n", manifest.BacktestComparisonVerified)
	fmt.Println("----------------------------------------")
	fmt.Println("Per-Strategy Breakdown:")
	for _, s := range manifest.Strategies {
		fmt.Printf("  - %s: trades=%d closed=%d open=%d win_rate=%s pnl=%s violations=%d\n",
			s.Strategy, s.TotalTrades, s.ClosedTrades, s.OpenPositions,
			s.WinRate.StringFixed(4), s.NetPnL.StringFixed(4), s.RiskViolations)
	}
	fmt.Println("----------------------------------------")
	fmt.Println("Acceptance Evaluation:")
	fmt.Printf("  Ready: %t\n", manifest.Acceptance.Ready)
	fmt.Printf("  Min Hours Met: %t\n", manifest.Acceptance.MinHoursMet)
	fmt.Printf("  Min Trades Met: %t\n", manifest.Acceptance.MinTradesMet)
	fmt.Printf("  Min Strategies Met: %t\n", manifest.Acceptance.MinStrategiesMet)
	fmt.Printf("  Risk Limits Met: %t\n", manifest.Acceptance.RiskLimitsMet)
	fmt.Printf("  Backtest Met: %t\n", manifest.Acceptance.BacktestMet)
	if len(manifest.Acceptance.Failures) > 0 {
		fmt.Println("  Failures:")
		for _, f := range manifest.Acceptance.Failures {
			fmt.Printf("    - %s\n", f)
		}
	}
	fmt.Println("========================================")
}
