package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// PaperTradingEvidence represents the readiness evidence artifact for paper trading.
type PaperTradingEvidence struct {
	Timestamp                  time.Time       `json:"timestamp"`
	ContinuousValidationHours  decimal.Decimal `json:"continuous_validation_hours"`
	StrategyCount              int             `json:"strategy_count"`
	ClosedTrades               int             `json:"closed_trades"`
	OpenPositions              int             `json:"open_positions"`
	Capital                    decimal.Decimal `json:"capital"`
	NetPnL                     decimal.Decimal `json:"net_pnl"`
	NormalizedPnL              decimal.Decimal `json:"normalized_pnl"`
	AvgNetPnL                  decimal.Decimal `json:"avg_net_pnl"`
	WinRate                    decimal.Decimal `json:"win_rate"`
	RiskLimitsEnforced         bool            `json:"risk_limits_enforced"`
	BacktestComparisonVerified bool            `json:"backtest_comparison_verified"`
	DiagnosticOnly             bool            `json:"diagnostic_only"`
	Strategies                 []string        `json:"strategies"`
	EvidenceFilePath           string          `json:"evidence_file_path"`
}

// PaperTradingEvidenceGenerator generates readiness evidence artifacts.
type PaperTradingEvidenceGenerator struct {
	db     DBPool
	logger Logger
}

// NewPaperTradingEvidenceGenerator creates a new evidence generator.
func NewPaperTradingEvidenceGenerator(db DBPool, logger Logger) *PaperTradingEvidenceGenerator {
	return &PaperTradingEvidenceGenerator{
		db:     db,
		logger: logger,
	}
}

// GenerateEvidence generates a readiness evidence artifact from paper trading data.
func (g *PaperTradingEvidenceGenerator) GenerateEvidence(
	startTime, endTime time.Time,
	strategies []string,
	capital decimal.Decimal,
) (*PaperTradingEvidence, error) {
	if g.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	// Build query with strategy filter
	args := []interface{}{startTime, endTime}
	strategyFilter := ""
	if len(strategies) > 0 {
		placeholders := make([]string, len(strategies))
		for i, s := range strategies {
			placeholders[i] = fmt.Sprintf("$%d", i+3)
			args = append(args, s)
		}
		strategyFilter = fmt.Sprintf(" AND strategy_id IN (%s)", strings.Join(placeholders, ","))
	}

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_trades,
			COUNT(CASE WHEN pnl > 0 THEN 1 END) as winning_trades,
			COALESCE(SUM(pnl), 0) as total_pnl,
			COALESCE(AVG(pnl), 0) as avg_pnl,
			COUNT(CASE WHEN status = 'closed' THEN 1 END) as closed_trades,
			COUNT(CASE WHEN status = 'open' THEN 1 END) as open_positions
		FROM paper_trades
		WHERE created_at BETWEEN $1 AND $2%s
	`, strategyFilter)

	var totalTrades, winningTrades, closedTrades, openPositions int
	var totalPnL, avgPnL decimal.Decimal

	ctx := context.Background()
	row := g.db.QueryRow(ctx, query, args...)
	err := row.Scan(
		&totalTrades, &winningTrades, &totalPnL, &avgPnL, &closedTrades, &openPositions,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query paper trades: %w", err)
	}

	// Calculate win rate
	var winRate decimal.Decimal
	if totalTrades > 0 {
		winRate = decimal.NewFromInt(int64(winningTrades)).Div(decimal.NewFromInt(int64(totalTrades)))
	}

	// Calculate continuous validation hours from the requested window
	continuousHours := decimal.NewFromFloat(endTime.Sub(startTime).Hours())

	// Check risk limits enforcement
	riskLimitsEnforced := g.checkRiskLimitsEnforcement(startTime, endTime)

	// Check backtest comparison
	backtestVerified := g.checkBacktestComparison(strategies)

	// Normalize PnL against provided capital
	normalizedPnL := decimal.Zero
	if capital.GreaterThan(decimal.Zero) {
		normalizedPnL = totalPnL.Div(capital)
	}

	evidence := &PaperTradingEvidence{
		Timestamp:                  time.Now(),
		ContinuousValidationHours:  continuousHours,
		StrategyCount:              len(strategies),
		ClosedTrades:               closedTrades,
		OpenPositions:              openPositions,
		Capital:                    capital,
		NetPnL:                     totalPnL,
		NormalizedPnL:              normalizedPnL,
		AvgNetPnL:                  avgPnL,
		WinRate:                    winRate,
		RiskLimitsEnforced:         riskLimitsEnforced,
		BacktestComparisonVerified: backtestVerified,
		DiagnosticOnly:             false,
		Strategies:                 strategies,
	}

	return evidence, nil
}

// checkRiskLimitsEnforcement checks if risk limits were enforced during validation.
func (g *PaperTradingEvidenceGenerator) checkRiskLimitsEnforcement(startTime, endTime time.Time) bool {
	if g.db == nil {
		return false
	}

	query := `
		SELECT COUNT(*) FROM paper_trades 
		WHERE created_at BETWEEN $1 AND $2 
		  AND status = 'closed'
		  AND (pnl < -0.05 * ABS(entry_price * size) OR size > 0.1 * ABS(entry_price * size))
	`

	var violations int
	ctx := context.Background()
	row := g.db.QueryRow(ctx, query, startTime, endTime)
	err := row.Scan(&violations)
	if err != nil {
		return false
	}

	return violations == 0
}

// checkBacktestComparison checks if backtest comparison was performed.
func (g *PaperTradingEvidenceGenerator) checkBacktestComparison(strategies []string) bool {
	if g.db == nil {
		return false
	}

	ctx := context.Background()
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

// SaveEvidence saves the evidence artifact to a JSON file.
func (g *PaperTradingEvidenceGenerator) SaveEvidence(evidence *PaperTradingEvidence, outputPath string) error {
	if outputPath == "" {
		outputPath = filepath.Join(os.TempDir(), fmt.Sprintf("paper-trading-evidence-%s.json", time.Now().Format("20060102-150405")))
	}

	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal evidence: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write evidence file: %w", err)
	}

	evidence.EvidenceFilePath = outputPath
	g.logger.Info(fmt.Sprintf("Paper trading evidence saved to: %s", outputPath))

	return nil
}

// PrintEvidence prints the evidence artifact to stdout.
func (g *PaperTradingEvidenceGenerator) PrintEvidence(evidence *PaperTradingEvidence) {
	fmt.Println("=== Paper Trading Readiness Evidence ===")
	fmt.Printf("Timestamp: %s\n", evidence.Timestamp.Format(time.RFC3339))
	fmt.Printf("Continuous Validation Hours: %s\n", evidence.ContinuousValidationHours.StringFixed(2))
	fmt.Printf("Strategy Count: %d\n", evidence.StrategyCount)
	fmt.Printf("Closed Trades: %d\n", evidence.ClosedTrades)
	fmt.Printf("Open Positions: %d\n", evidence.OpenPositions)
	fmt.Printf("Net PnL: %s\n", evidence.NetPnL.StringFixed(4))
	fmt.Printf("Avg Net PnL: %s\n", evidence.AvgNetPnL.StringFixed(4))
	fmt.Printf("Win Rate: %s\n", evidence.WinRate.StringFixed(4))
	fmt.Printf("Risk Limits Enforced: %t\n", evidence.RiskLimitsEnforced)
	fmt.Printf("Backtest Comparison Verified: %t\n", evidence.BacktestComparisonVerified)
	fmt.Printf("Diagnostic Only: %t\n", evidence.DiagnosticOnly)
	fmt.Printf("Strategies: %v\n", evidence.Strategies)
	if evidence.EvidenceFilePath != "" {
		fmt.Printf("Evidence File: %s\n", evidence.EvidenceFilePath)
	}
	fmt.Println("========================================")
}
