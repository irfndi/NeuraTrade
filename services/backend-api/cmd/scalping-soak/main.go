package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "scalping-soak: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dbPath          string
		exchange        string
		chatID          string
		orderPrefix     string
		cycles          int
		intervalMS      int
		timeoutSeconds  int
		requireTrades   bool
		initialCapital  string
		feeRate         string
		includeBaseline bool
		minTrades       int
		minWinRate      string
		minNetPnL       string
		minAvgNetPnL    string
	)

	flags := flag.NewFlagSet("scalping-soak", flag.ExitOnError)
	flags.StringVar(&dbPath, "db", "", "SQLite database path for persisted soak telemetry")
	flags.StringVar(&exchange, "exchange", "bitget", "public exchange to probe")
	flags.StringVar(&chatID, "chat-id", envString("NEURATRADE_SCALPING_SOAK_CHAT_ID", "operator-scalping-soak"), "chat id for persisted soak telemetry")
	flags.StringVar(&orderPrefix, "order-prefix", envString("NEURATRADE_SCALPING_SOAK_ORDER_PREFIX", "operator-scalping-soak"), "order prefix for persisted soak telemetry")
	flags.IntVar(&cycles, "cycles", services.DefaultScalpingLivePaperSoakCycles, "number of public-data paper soak cycles")
	flags.IntVar(&intervalMS, "interval-ms", 2000, "delay between cycles in milliseconds")
	flags.IntVar(&timeoutSeconds, "timeout-seconds", 0, "overall timeout; defaults to cycles and interval")
	flags.BoolVar(&requireTrades, "require-trades", false, "fail if the paper soak produces zero closed paper trades")
	flags.StringVar(&initialCapital, "capital", "48", "initial paper capital in USDT")
	flags.StringVar(&feeRate, "fee-rate", "0.0006", "round-trip fee-rate input used by the paper simulator")
	flags.BoolVar(&includeBaseline, "baseline", true, "include the broken live scalping baseline comparison")
	flags.IntVar(&minTrades, "min-trades", 0, "fail unless the soak produces at least this many closed paper trades")
	flags.StringVar(&minWinRate, "min-win-rate", "", "fail unless report win_rate is at least this decimal value")
	flags.StringVar(&minNetPnL, "min-net-pnl", "", "fail unless report net_pnl is at least this decimal value")
	flags.StringVar(&minAvgNetPnL, "min-avg-net-pnl", "", "fail unless avg_net_pnl_per_trade is at least this decimal value")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}

	if dbPath == "" {
		dbPath = filepath.Join(os.TempDir(), fmt.Sprintf("neuratrade-scalping-soak-%d.db", time.Now().UnixNano()))
	}
	db, err := database.NewSQLiteConnection(dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite database %s: %w", dbPath, err)
	}
	defer func() {
		_ = db.Close()
	}()

	capital, err := decimal.NewFromString(initialCapital)
	if err != nil {
		return fmt.Errorf("parse --capital: %w", err)
	}
	fees, err := decimal.NewFromString(feeRate)
	if err != nil {
		return fmt.Errorf("parse --fee-rate: %w", err)
	}

	interval := time.Duration(intervalMS) * time.Millisecond
	timeout := services.ScalpingLivePaperSoakTimeout(cycles, interval)
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var baseline *services.ScalpingSoakBaseline
	if includeBaseline {
		value := services.BrokenScalpingBaseline()
		baseline = &value
	}
	result, err := services.RunPublicScalpingLivePaperSoak(ctx, db, services.ScalpingLivePaperSoakOptions{
		Exchange:       exchange,
		Cycles:         cycles,
		Interval:       interval,
		ChatID:         chatID,
		OrderPrefix:    orderPrefix,
		RequireTrades:  requireTrades,
		InitialCapital: capital,
		FeeRate:        fees,
		Baseline:       baseline,
	})
	if err != nil {
		return err
	}
	if err := validateAcceptanceGates(result, acceptanceGateOptions{
		MinTrades:    minTrades,
		MinWinRate:   minWinRate,
		MinNetPnL:    minNetPnL,
		MinAvgNetPnL: minAvgNetPnL,
	}); err != nil {
		return err
	}

	payload := struct {
		DBPath string                                `json:"db_path"`
		Result *services.ScalpingLivePaperSoakResult `json:"result"`
	}{
		DBPath: dbPath,
		Result: result,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

type acceptanceGateOptions struct {
	MinTrades    int
	MinWinRate   string
	MinNetPnL    string
	MinAvgNetPnL string
}

func validateAcceptanceGates(result *services.ScalpingLivePaperSoakResult, options acceptanceGateOptions) error {
	if result == nil {
		return fmt.Errorf("acceptance gates require soak result")
	}
	report := result.Report
	if options.MinTrades > 0 && report.TradeSummary.ClosedTrades < options.MinTrades {
		return fmt.Errorf("acceptance gate failed: closed_trades=%d below min_trades=%d", report.TradeSummary.ClosedTrades, options.MinTrades)
	}
	if err := validateMinDecimalGate("min-win-rate", "win_rate", report.TradeSummary.WinRate, options.MinWinRate); err != nil {
		return err
	}
	if err := validateMinDecimalGate("min-net-pnl", "net_pnl", report.TradeSummary.NetPnL, options.MinNetPnL); err != nil {
		return err
	}
	if err := validateMinDecimalGate("min-avg-net-pnl", "avg_net_pnl_per_trade", report.TradeSummary.AvgNetPnLPerTrade, options.MinAvgNetPnL); err != nil {
		return err
	}
	return nil
}

func validateMinDecimalGate(flagName string, metricName string, actual decimal.Decimal, rawMinimum string) error {
	if rawMinimum == "" {
		return nil
	}
	minimum, err := decimal.NewFromString(rawMinimum)
	if err != nil {
		return fmt.Errorf("parse --%s: %w", flagName, err)
	}
	if actual.LessThan(minimum) {
		return fmt.Errorf("acceptance gate failed: %s=%s below minimum=%s", metricName, actual.String(), minimum.String())
	}
	return nil
}

func envString(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
