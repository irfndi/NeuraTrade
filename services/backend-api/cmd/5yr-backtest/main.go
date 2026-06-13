package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		os.Exit(1)
	}
}

func run() (err error) {
	var (
		startStr          = flag.String("start", "2021-06-01", "Start date (YYYY-MM-DD)")
		endStr            = flag.String("end", "2026-06-01", "End date (YYYY-MM-DD)")
		symbols           = flag.String("symbols", "BTC/USDT,ETH/USDT,SOL/USDT,BNB/USDT", "Comma-separated symbols")
		persist           = flag.Bool("persist", true, "Persist results to database")
		minExpectancyN    = flag.Int("min-expectancy-n", 0, "Min samples for expectancy gate (0 = default: 8)")
		minExpectancyEdge = flag.Float64("min-expectancy-edge", 0, "Min edge for expectancy gate (0 = default: 0.001)")
		maxLossPct        = flag.Float64("max-loss-pct", 0, "Hard max-loss per trade as decimal fraction (0 = default: 0.015 = 1.5%)")
	)
	flag.Parse()

	startTime, err := time.Parse("2006-01-02", *startStr)
	if err != nil {
		return fmt.Errorf("parse start date: %w", err)
	}
	endTime, err := time.Parse("2006-01-02", *endStr)
	if err != nil {
		return fmt.Errorf("parse end date: %w", err)
	}

	symbolList := []string{}
	for _, s := range splitComma(*symbols) {
		if s != "" {
			symbolList = append(symbolList, s)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.NewDatabaseConnection(&cfg.Database)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			wrapped := fmt.Errorf("close db: %w", closeErr)
			if err == nil {
				err = wrapped
				return
			}
			err = errors.Join(err, wrapped)
		}
	}()

	dbPool, ok := db.(services.DBPool)
	if !ok {
		return fmt.Errorf("db does not implement DBPool")
	}

	initialCapital, _ := decimal.NewFromString("10000")

	svcConfig := services.ScalpingBacktestConfig{
		StartTime:          startTime,
		EndTime:            endTime,
		Symbols:            symbolList,
		Exchange:           "binance",
		InitialCapital:     initialCapital,
		FeeRate:            decimal.NewFromFloat(0.0002),
		MaxBidAskSpreadPct: 0.08,
		MinConfidence:      0.60,
		MinExpectancyN:     *minExpectancyN,
		MinExpectancyEdge:  *minExpectancyEdge,
		SpreadMultiplier:   8,
		MaxCapitalPct:      25.0,
		DefaultHoldPeriod:  4 * time.Hour,
		Mode:               "deterministic",
		MaxLossPct:         *maxLossPct,
	}

	fmt.Printf("Starting backtest...\n")
	fmt.Printf("  Range: %s → %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	fmt.Printf("  Symbols: %v\n", svcConfig.Symbols)
	fmt.Printf("  Exchange: %s\n", svcConfig.Exchange)
	fmt.Printf("  Initial Capital: %s USDT\n", initialCapital.String())
	fmt.Printf("  Mode: %s\n", svcConfig.Mode)
	fmt.Printf("  MinExpectancyN: %d\n", svcConfig.MinExpectancyN)
	fmt.Printf("  MinExpectancyEdge: %f\n", svcConfig.MinExpectancyEdge)
	fmt.Println()

	engine := services.NewScalpingBacktestEngine(dbPool, svcConfig)
	result, err := engine.Run(context.Background())
	if err != nil {
		return fmt.Errorf("engine run: %w", err)
	}

	fmt.Println("=== BACKTEST RESULTS ===")
	fmt.Printf("Mode: %s\n", result.Mode)
	fmt.Printf("Duration: %s\n", result.EndTime.Sub(result.StartTime))
	fmt.Println()
	fmt.Printf("Total Signals: %d\n", result.Summary.TotalSignals)
	fmt.Printf("Eligible Signals: %d\n", result.Summary.EligibleSignals)
	fmt.Printf("Rejected Signals: %d\n", result.Summary.RejectedSignals)
	fmt.Printf("Total Trades: %d\n", result.Summary.TotalTrades)
	fmt.Printf("Open Positions: %d\n", result.Summary.OpenPositions)
	fmt.Printf("Winning Trades: %d\n", result.Summary.WinningTrades)
	fmt.Printf("Losing Trades: %d\n", result.Summary.LosingTrades)
	fmt.Printf("Win Rate: %s\n", result.Summary.WinRate.String())
	fmt.Printf("Total PnL: %s USDT\n", result.Summary.TotalPnL.String())
	fmt.Printf("Total Return %%: %s\n", result.Summary.TotalReturnPct.String())
	fmt.Printf("Profit Factor: %s\n", result.Summary.ProfitFactor.String())
	fmt.Printf("Sharpe Ratio: %s\n", result.Summary.SharpeRatio.String())
	fmt.Printf("Max Drawdown: %s%%\n", result.Summary.MaxDrawdownPct.String())
	fmt.Printf("Avg Win: %s\n", result.Summary.AvgWin.String())
	fmt.Printf("Avg Loss: %s\n", result.Summary.AvgLoss.String())
	fmt.Println()
	fmt.Printf("Signals processed: %d\n", len(result.Signals))
	fmt.Printf("Trades executed: %d\n", len(result.Trades))

	if len(result.GateSummary) > 0 {
		fmt.Println("\n=== GATE SUMMARY ===")
		for _, g := range result.GateSummary {
			fmt.Printf("  %s: pass=%d reject=%d\n", g.GateName, g.PassCount, g.RejectCount)
		}
	}

	if len(result.Trades) > 0 {
		fmt.Println("\n=== SAMPLE TRADES ===")
		for i, t := range result.Trades[:min(5, len(result.Trades))] {
			fmt.Printf("  %d. %s %s: entry=%s exit=%s pnl=%s\n",
				i+1, t.Symbol, t.Side, t.EntryPrice, t.ExitPrice, t.PnL)
		}
	}

	if *persist {
		runID := fmt.Sprintf("backtest-%d", time.Now().Unix())
		if err := persistFullResult(dbPool, runID, svcConfig, result); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: failed to persist full result: %v\n", err)
		} else {
			fmt.Printf("\nResults persisted to DB as run_id: %s\n", runID)
		}
	}
	return nil
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func persistFullResult(db services.DBPool, runID string, config services.ScalpingBacktestConfig, result *services.ScalpingBacktestResult) error {
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	completedAt := time.Now().UTC()
	summaryRaw, _ := json.Marshal(result.Summary)
	configRaw, _ := json.Marshal(map[string]interface{}{
		"start_time": config.StartTime,
		"end_time":   config.EndTime,
		"symbols":    config.Symbols,
		"exchange":   config.Exchange,
		"mode":       config.Mode,
	})

	if _, err := tx.Exec(ctx, `
		INSERT INTO scalping_backtest_runs (id, status, config, summary, created_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, runID, "completed", configRaw, summaryRaw, completedAt, completedAt); err != nil {
		return fmt.Errorf("insert run: %w", err)
	}

	for _, t := range result.Trades {
		holdDuration := int(t.ExitTime.Sub(t.EntryTime).Seconds())
		signalID := uuid.NewString()
		signalRaw, _ := json.Marshal(map[string]interface{}{
			"symbol":    t.Symbol,
			"timestamp": t.EntryTime.Format(time.RFC3339),
		})
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_signals (
				id, run_id, timestamp, symbol, exchange, signal, regime, regime_volatility,
				funnel_stage, rejection_reason, gate_results
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, signalID, runID, t.EntryTime, t.Symbol, t.Exchange, signalRaw, t.RegimeAtEntry, "normal", "executed", "", "{}"); err != nil {
			return fmt.Errorf("insert signal %s: %w", signalID, err)
		}
		tradeID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_trades (
				id, run_id, signal_id, symbol, exchange, side, size, notional,
				entry_price, exit_price, entry_timestamp, exit_timestamp,
				pnl, pnl_pct, fees, outcome, exit_reason,
				regime_at_entry, regime_at_exit, hold_duration_seconds
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		`, tradeID, runID, signalID, t.Symbol, t.Exchange, t.Side, t.Size, t.Notional,
			t.EntryPrice, t.ExitPrice, t.EntryTime, t.ExitTime,
			t.PnL, t.PnLPct, t.Fees, t.Outcome, t.ExitReason,
			t.RegimeAtEntry, t.RegimeAtExit, holdDuration); err != nil {
			return fmt.Errorf("insert trade %s: %w", tradeID, err)
		}
	}

	for _, g := range result.GateSummary {
		reasonsRaw, _ := json.Marshal(g.TopRejectionReasons)
		bySymbolRaw, _ := json.Marshal(g.BreakdownBySymbol)
		byRegimeRaw, _ := json.Marshal(g.BreakdownByRegime)
		if _, err := tx.Exec(ctx, `
			INSERT INTO scalping_backtest_gate_summary (
				id, run_id, gate_name, pass_count, reject_count,
				top_rejection_reasons, breakdown_by_symbol, breakdown_by_regime
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, uuid.NewString(), runID, g.GateName, g.PassCount, g.RejectCount, reasonsRaw, bySymbolRaw, byRegimeRaw); err != nil {
			return fmt.Errorf("insert gate summary %s: %w", g.GateName, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
