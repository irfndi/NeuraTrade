package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "paper-readiness: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		startTimeStr   string
		endTimeStr     string
		strategies     string
		outputPath     string
		failIfNotReady bool
	)

	flag.StringVar(&startTimeStr, "start", "", "Start time (RFC3339); defaults to 7 days ago")
	flag.StringVar(&endTimeStr, "end", "", "End time (RFC3339); defaults to now")
	flag.StringVar(&strategies, "strategies", "scalping,daily_trading,swing_trading,arbitrage", "Comma-separated strategies")
	flag.StringVar(&outputPath, "output", "", "Output path for manifest JSON")
	flag.BoolVar(&failIfNotReady, "fail-if-not-ready", false, "Exit with non-zero code if manifest is not ready")
	flag.Parse()

	// Parse times
	startTime := time.Now().Add(-7 * 24 * time.Hour)
	if startTimeStr != "" {
		var err error
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			return fmt.Errorf("invalid start time: %w", err)
		}
	}

	endTime := time.Now()
	if endTimeStr != "" {
		var err error
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			return fmt.Errorf("invalid end time: %w", err)
		}
	}

	strategyList := parseStrategies(strategies)

	// Load config and connect to database
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	db, err := database.NewDatabaseConnection(&cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	// Generate unified readiness manifest
	ctx := context.Background()
	generator := services.NewReadinessManifestGenerator(db, &simpleLogger{})
	manifest, err := generator.GenerateManifest(ctx, startTime, endTime, strategyList)
	if err != nil {
		return fmt.Errorf("failed to generate manifest: %w", err)
	}

	// Print to stdout
	generator.PrintManifest(manifest)

	// Save to file
	if outputPath == "" {
		outputPath = fmt.Sprintf("paper-trading-readiness-%s.json", time.Now().Format("20060102-150405"))
	}
	if err := generator.SaveManifest(manifest, outputPath); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	fmt.Printf("\nManifest saved to: %s\n", outputPath)

	if failIfNotReady && !manifest.Acceptance.Ready {
		return fmt.Errorf("paper trading NOT READY: %s", strings.Join(manifest.Acceptance.Failures, "; "))
	}

	return nil
}

type simpleLogger struct{}

func (s *simpleLogger) WithFields(_ map[string]interface{}) services.Logger { return s }
func (s *simpleLogger) Info(msg string)                                     { fmt.Println("[INFO]", msg) }
func (s *simpleLogger) Warn(msg string)                                     { fmt.Println("[WARN]", msg) }
func (s *simpleLogger) Error(msg string)                                    { fmt.Println("[ERROR]", msg) }

func parseStrategies(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}
