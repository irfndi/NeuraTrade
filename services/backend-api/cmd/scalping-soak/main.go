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

func envString(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
