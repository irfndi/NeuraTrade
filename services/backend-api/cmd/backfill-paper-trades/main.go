package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "backfill-paper-trades: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		startStr string
		endStr   string
		capital  string
	)
	flag.StringVar(&startStr, "start", "", "Start time (RFC3339); defaults to 30 days ago")
	flag.StringVar(&endStr, "end", "", "End time (RFC3339); defaults to now")
	flag.StringVar(&capital, "capital", "10000", "Initial capital per strategy")
	flag.Parse()

	startTime := time.Now().Add(-30 * 24 * time.Hour)
	if startStr != "" {
		var err error
		startTime, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			return fmt.Errorf("invalid start time: %w", err)
		}
	}

	endTime := time.Now()
	if endStr != "" {
		var err error
		endTime, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return fmt.Errorf("invalid end time: %w", err)
		}
	}

	initialCapital, err := decimal.NewFromString(capital)
	if err != nil {
		return fmt.Errorf("invalid capital: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	db, err := database.NewDatabaseConnection(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() { _ = db.Close() }()

	dbPool, ok := db.(database.DBPool)
	if !ok {
		return fmt.Errorf("database does not implement DBPool")
	}

	recorder := services.NewPaperTradeRecorder(dbPool, &simpleLogger{})
	executor := services.NewPaperExecutionSimulator(services.DefaultPaperExecutionConfig())

	strategies := services.DefaultPaperTradingStrategies()
	for i := range strategies {
		strategies[i].MinConfidence = 0.001 // Very low so small synthetic candles fire signals
	}

	backfill := services.NewPaperTradingBackfillValidation(
		dbPool,
		executor,
		recorder,
		services.PaperTradingBackfillConfig{
			StartTime:          startTime,
			EndTime:            endTime,
			Exchange:           "binance",
			InitialCapital:     initialCapital,
			Strategies:         strategies,
			ExecutionConfig:    services.DefaultPaperExecutionConfig(),
			MinContinuousHours: 168,
			MinStrategies:      4,
			MinClosedTrades:    10,
			MinWinRatePct:      0,
			MaxDrawdownPct:     50,
		},
		&simpleLogger{},
	)

	ctx := context.Background()
	result, err := backfill.Run(ctx)
	if err != nil {
		return fmt.Errorf("backfill failed: %w", err)
	}

	fmt.Println("=== Backfill Results ===")
	fmt.Printf("Run ID: %s\n", result.RunID)
	fmt.Printf("Candles Processed: %d\n", result.CandlesProcessed)
	fmt.Printf("Closed Trades: %d\n", result.ClosedTrades)
	fmt.Printf("Open Trades: %d\n", result.OpenTrades)
	fmt.Printf("Net PnL: %s\n", result.NetPnL.StringFixed(4))
	fmt.Printf("Max Drawdown %%: %s\n", result.MaxDrawdownPct.StringFixed(4))
	fmt.Printf("Win Rate: %s\n", result.WinRate.StringFixed(4))
	fmt.Printf("Validation Passed: %t\n", result.ValidationPassed)
	fmt.Printf("Blockers: %d\n", len(result.BlockerStatuses))
	for _, b := range result.BlockerStatuses {
		fmt.Printf("  - %s: satisfied=%t current=%s required=%s evidence=%s\n", b.BlockerID, b.Satisfied, b.CurrentValue, b.Required, b.Evidence)
	}

	manifest, err := backfill.Manifests(result)
	if err != nil {
		return fmt.Errorf("manifest generation failed: %w", err)
	}

	fmt.Printf("\nManifest generated (%d bytes)\n", len(manifest))
	return nil
}

type simpleLogger struct{}

func (s *simpleLogger) WithFields(_ map[string]interface{}) services.Logger { return s }
func (s *simpleLogger) Info(msg string)                                     { fmt.Println("[INFO]", msg) }
func (s *simpleLogger) Warn(msg string)                                     { fmt.Println("[WARN]", msg) }
func (s *simpleLogger) Error(msg string)                                    { fmt.Println("[ERROR]", msg) }
