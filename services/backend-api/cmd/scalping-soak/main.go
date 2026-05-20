package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
		dbPath                   string
		outputPath               string
		exchange                 string
		chatID                   string
		orderPrefix              string
		cycles                   int
		intervalMS               int
		timeoutSeconds           int
		holdPeriodSeconds        int
		maxPairs                 int
		maxCandidates            int
		orderBookPairs           int
		requireTrades            bool
		initialCapital           string
		feeRate                  string
		includeBaseline          bool
		minTrades                int
		minWinRate               string
		minNetPnL                string
		minAvgNetPnL             string
		minSignalQualityCoverage string
		maxHoldRatio             string
		maxDrawdown              string
		maxDrawdownPct           string
		maxAIDegradedCycles      string
		maxPerfectWinTrades      string
		minBaselineWinRateDelta  string
		minBaselineNetPnLDelta   string
		minBaselineAvgPnLDelta   string
		requireLiveTrialReady    bool
		recordRolloutProof       bool
		strategyID               string
	)

	flags := flag.NewFlagSet("scalping-soak", flag.ExitOnError)
	flags.StringVar(&dbPath, "db", "", "SQLite database path for persisted soak telemetry")
	flags.StringVar(&outputPath, "output", "", "optional path for a clean JSON result artifact")
	flags.StringVar(&exchange, "exchange", "bitget", "public exchange to probe")
	flags.StringVar(&chatID, "chat-id", envString("NEURATRADE_SCALPING_SOAK_CHAT_ID", "operator-scalping-soak"), "chat id for persisted soak telemetry")
	flags.StringVar(&orderPrefix, "order-prefix", envString("NEURATRADE_SCALPING_SOAK_ORDER_PREFIX", "operator-scalping-soak"), "order prefix for persisted soak telemetry")
	flags.IntVar(&cycles, "cycles", services.DefaultScalpingLivePaperSoakCycles, "number of public-data paper soak cycles")
	flags.IntVar(&intervalMS, "interval-ms", 2000, "delay between cycles in milliseconds")
	flags.IntVar(&timeoutSeconds, "timeout-seconds", 0, "overall timeout; defaults to cycles and interval")
	flags.IntVar(&holdPeriodSeconds, "hold-period-seconds", 0, "paper position hold period in seconds; 0 uses the default")
	flags.IntVar(&maxPairs, "max-pairs", envInt("NEURATRADE_SCALPING_MAX_PAIRS", 0), "maximum pairs to analyze per cycle; 0 uses scalping config default")
	flags.IntVar(&maxCandidates, "max-candidates", envInt("NEURATRADE_SCALPING_MAX_CANDIDATES", 0), "maximum discovered candidates to score; 0 uses scalping config default")
	flags.IntVar(&orderBookPairs, "orderbook-pairs", envInt("NEURATRADE_SCALPING_ORDERBOOK_PAIRS", 0), "maximum pairs with orderbook quality per cycle; 0 uses scalping config default")
	flags.BoolVar(&requireTrades, "require-trades", false, "fail if the paper soak produces zero closed paper trades")
	flags.StringVar(&initialCapital, "capital", "48", "initial paper capital in USDT")
	flags.StringVar(&feeRate, "fee-rate", "0.0006", "round-trip fee-rate input used by the paper simulator")
	flags.BoolVar(&includeBaseline, "baseline", true, "include the broken live scalping baseline comparison")
	flags.IntVar(&minTrades, "min-trades", 0, "fail unless the soak produces at least this many closed paper trades")
	flags.StringVar(&minWinRate, "min-win-rate", "", "fail unless report win_rate is at least this decimal value")
	flags.StringVar(&minNetPnL, "min-net-pnl", "", "fail unless report net_pnl is at least this decimal value")
	flags.StringVar(&minAvgNetPnL, "min-avg-net-pnl", "", "fail unless avg_net_pnl_per_trade is at least this decimal value")
	flags.StringVar(&minSignalQualityCoverage, "min-signal-quality-coverage", "", "fail unless signal_quality.coverage is at least this decimal value")
	flags.StringVar(&maxHoldRatio, "max-hold-ratio", "", "fail unless action_split.hold is at or below this decimal value")
	flags.StringVar(&maxDrawdown, "max-drawdown", "", "fail unless max_drawdown is at or below this decimal value")
	flags.StringVar(&maxDrawdownPct, "max-drawdown-pct", "", "fail unless max_drawdown_pct is at or below this decimal value")
	flags.StringVar(&maxAIDegradedCycles, "max-ai-provider-degraded-cycles", "", "maximum AI provider degraded cycles allowed; empty disables this gate")
	flags.StringVar(&maxPerfectWinTrades, "max-perfect-win-trades", "", "maximum closed trades allowed with 100% wins and zero drawdown; empty disables this paper-realism gate")
	flags.StringVar(&minBaselineWinRateDelta, "min-baseline-win-rate-delta", "", "fail unless win-rate delta versus baseline is at least this decimal value")
	flags.StringVar(&minBaselineNetPnLDelta, "min-baseline-net-pnl-delta", "", "fail unless net-PnL delta versus baseline is at least this decimal value")
	flags.StringVar(&minBaselineAvgPnLDelta, "min-baseline-avg-pnl-delta", "", "fail unless avg-PnL-per-trade delta versus baseline is at least this decimal value")
	flags.BoolVar(&requireLiveTrialReady, "require-live-trial-ready", false, "fail unless paper evidence is ready for a tightly capped live/testnet trial")
	flags.BoolVar(&recordRolloutProof, "record-rollout-proof", false, "persist live-ready paper proof metrics into the autonomy rollout state")
	flags.StringVar(&strategyID, "strategy-id", "", "strategy id for --record-rollout-proof; empty uses the chat scalping strategy id")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if strategyID == "" {
		strategyID = services.ScalpingStrategyID(chatID)
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
	if holdPeriodSeconds < 0 {
		return fmt.Errorf("invalid --hold-period-seconds value %d: must be zero or greater", holdPeriodSeconds)
	}
	holdPeriod := time.Duration(holdPeriodSeconds) * time.Second

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
		Exchange:          exchange,
		Cycles:            cycles,
		Interval:          interval,
		ChatID:            chatID,
		OrderPrefix:       orderPrefix,
		RequireTrades:     requireTrades,
		InitialCapital:    capital,
		FeeRate:           fees,
		HoldPeriod:        holdPeriod,
		MaxPairsToAnalyze: maxPairs,
		MaxCandidatePairs: maxCandidates,
		OrderBookPairs:    orderBookPairs,
		Baseline:          baseline,
	})
	if err != nil {
		return err
	}
	if err := validateAcceptanceGates(result, acceptanceGateOptions{
		MinTrades:                minTrades,
		MinWinRate:               minWinRate,
		MinNetPnL:                minNetPnL,
		MinAvgNetPnL:             minAvgNetPnL,
		MinSignalQualityCoverage: minSignalQualityCoverage,
		MaxHoldRatio:             maxHoldRatio,
		MaxDrawdown:              maxDrawdown,
		MaxDrawdownPct:           maxDrawdownPct,
		MaxAIDegradedCycles:      maxAIDegradedCycles,
		MaxPerfectWinTrades:      maxPerfectWinTrades,
		MinBaselineWinRateDelta:  minBaselineWinRateDelta,
		MinBaselineNetPnLDelta:   minBaselineNetPnLDelta,
		MinBaselineAvgPnLDelta:   minBaselineAvgPnLDelta,
		RequireLiveTrialReady:    requireLiveTrialReady,
	}); err != nil {
		if encodeErr := writeResultPayloads(dbPath, outputPath, result); encodeErr != nil {
			return fmt.Errorf("%w; also failed to write soak result JSON: %v", err, encodeErr)
		}
		return err
	}

	if recordRolloutProof {
		if _, err := services.RecordScalpingLiveTrialProof(ctx, db.DB, strategyID, result); err != nil {
			if encodeErr := writeResultPayloads(dbPath, outputPath, result); encodeErr != nil {
				return fmt.Errorf("%w; also failed to write soak result JSON: %v", err, encodeErr)
			}
			return err
		}
	}

	return writeResultPayloads(dbPath, outputPath, result)
}

func writeResultPayloads(dbPath, outputPath string, result *services.ScalpingLivePaperSoakResult) error {
	if err := writeResultPayload(os.Stdout, dbPath, result); err != nil {
		return err
	}
	if outputPath == "" {
		return nil
	}
	return writeResultPayloadFile(outputPath, dbPath, result)
}

func writeResultPayload(out io.Writer, dbPath string, result *services.ScalpingLivePaperSoakResult) error {
	payload := struct {
		DBPath string                                `json:"db_path"`
		Result *services.ScalpingLivePaperSoakResult `json:"result"`
	}{
		DBPath: dbPath,
		Result: result,
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func writeResultPayloadFile(outputPath, dbPath string, result *services.ScalpingLivePaperSoakResult) error {
	if outputPath == "" {
		return nil
	}
	dir := filepath.Dir(outputPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create output directory %s: %w", dir, err)
		}
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(outputPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create output temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := writeResultPayload(tmp, dbPath, result); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close output temp file: %w", err)
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return fmt.Errorf("move output artifact into place: %w", err)
	}
	cleanup = false
	return nil
}

type acceptanceGateOptions struct {
	MinTrades                int
	MinWinRate               string
	MinNetPnL                string
	MinAvgNetPnL             string
	MinSignalQualityCoverage string
	MaxHoldRatio             string
	MaxDrawdown              string
	MaxDrawdownPct           string
	MaxAIDegradedCycles      string
	MaxPerfectWinTrades      string
	MinBaselineWinRateDelta  string
	MinBaselineNetPnLDelta   string
	MinBaselineAvgPnLDelta   string
	RequireLiveTrialReady    bool
}

// validateAcceptanceGates applies CLI-configured proof thresholds to a completed scalping soak report.
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
	if err := validateMinDecimalGate("min-signal-quality-coverage", "signal_quality.coverage", report.SignalQuality.Coverage, options.MinSignalQualityCoverage); err != nil {
		return err
	}
	if options.MaxHoldRatio != "" {
		holdRatio := decimal.Zero
		if value, ok := decimalValueFromMap(report.ActionSplit, "hold"); ok {
			holdRatio = value
		}
		if err := validateMaxRatioGate("max-hold-ratio", "action_split.hold", holdRatio, options.MaxHoldRatio); err != nil {
			return err
		}
	}
	if err := validateMaxDecimalGate("max-drawdown", "max_drawdown", report.TradeSummary.MaxDrawdown, options.MaxDrawdown); err != nil {
		return err
	}
	if options.MaxDrawdownPct != "" && report.BaselineComparison == nil {
		return fmt.Errorf("--max-drawdown-pct requires --baseline=true")
	}
	if err := validateMaxDecimalGate("max-drawdown-pct", "max_drawdown_pct", report.TradeSummary.MaxDrawdownPct, options.MaxDrawdownPct); err != nil {
		return err
	}
	if err := validateMaxIntGate("max-ai-provider-degraded-cycles", "ai_provider_degraded_cycles", report.AIProviderDegradation.DegradedCycles, options.MaxAIDegradedCycles); err != nil {
		return err
	}
	if err := validatePerfectWinRealismGate(report.TradeSummary, options.MaxPerfectWinTrades); err != nil {
		return err
	}
	if err := validateBaselineDeltaGates(report.BaselineComparison, options); err != nil {
		return err
	}
	if options.RequireLiveTrialReady && !report.LiveTrialReadiness.Ready {
		return fmt.Errorf("acceptance gate failed: live_trial_readiness.ready=false reasons=%v", report.LiveTrialReadiness.Reasons)
	}
	return nil
}

// validatePerfectWinRealismGate rejects high-sample paper results that report only wins with no drawdown.
func validatePerfectWinRealismGate(summary services.ScalpingSoakTradeSummary, rawMaximum string) error {
	if rawMaximum == "" {
		return nil
	}
	maximum, err := strconv.Atoi(rawMaximum)
	if err != nil {
		return fmt.Errorf("parse --max-perfect-win-trades value %q: %w", rawMaximum, err)
	}
	if maximum < 0 {
		return fmt.Errorf("invalid --max-perfect-win-trades value %q: must be zero or greater", rawMaximum)
	}
	if summary.ClosedTrades <= maximum || summary.ClosedTrades <= 0 {
		return nil
	}
	if summary.Wins == summary.ClosedTrades &&
		summary.Losses == 0 &&
		summary.MaxDrawdown.IsZero() &&
		summary.MaxDrawdownPct.IsZero() {
		return fmt.Errorf(
			"paper realism gate failed: closed_trades=%d wins=%d losses=%d max_drawdown_pct=%s exceeds max_perfect_win_trades=%d; perfect paper wins without drawdown are insufficient proof",
			summary.ClosedTrades,
			summary.Wins,
			summary.Losses,
			summary.MaxDrawdownPct.String(),
			maximum,
		)
	}
	return nil
}

func validateBaselineDeltaGates(comparison *services.ScalpingSoakBaselineComparison, options acceptanceGateOptions) error {
	if options.MinBaselineWinRateDelta == "" && options.MinBaselineNetPnLDelta == "" && options.MinBaselineAvgPnLDelta == "" {
		return nil
	}
	if comparison == nil {
		return fmt.Errorf("baseline delta gates require --baseline=true")
	}
	if err := validateMinDecimalGate("min-baseline-win-rate-delta", "baseline.delta_win_rate", comparison.DeltaWinRate, options.MinBaselineWinRateDelta); err != nil {
		return err
	}
	if err := validateMinDecimalGate("min-baseline-net-pnl-delta", "baseline.delta_net_pnl", comparison.DeltaNetPnL, options.MinBaselineNetPnLDelta); err != nil {
		return err
	}
	if err := validateMinDecimalGate("min-baseline-avg-pnl-delta", "baseline.delta_avg_pnl_per_trade", comparison.DeltaAvgPnLPerTrade, options.MinBaselineAvgPnLDelta); err != nil {
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

func validateMaxDecimalGate(flagName string, metricName string, actual decimal.Decimal, rawMaximum string) error {
	if rawMaximum == "" {
		return nil
	}
	maximum, err := decimal.NewFromString(rawMaximum)
	if err != nil {
		return fmt.Errorf("parse --%s value %q: %w", flagName, rawMaximum, err)
	}
	if maximum.IsNegative() {
		return fmt.Errorf("invalid --%s value %q: must be zero or greater", flagName, rawMaximum)
	}
	if actual.GreaterThan(maximum) {
		return fmt.Errorf("acceptance gate failed: %s=%q above maximum=%q", metricName, actual.String(), maximum.String())
	}
	return nil
}

func validateMaxRatioGate(flagName string, metricName string, actual decimal.Decimal, rawMaximum string) error {
	if rawMaximum == "" {
		return nil
	}
	maximum, err := decimal.NewFromString(rawMaximum)
	if err != nil {
		return fmt.Errorf("parse --%s value %q: %w", flagName, rawMaximum, err)
	}
	if maximum.IsNegative() {
		return fmt.Errorf("invalid --%s value %q: must be zero or greater", flagName, rawMaximum)
	}
	if maximum.GreaterThan(decimal.NewFromInt(1)) {
		return fmt.Errorf("invalid --%s value %q: must be at most 1", flagName, rawMaximum)
	}
	if actual.GreaterThan(maximum) {
		return fmt.Errorf("acceptance gate failed: %s=%q above maximum=%q", metricName, actual.String(), maximum.String())
	}
	return nil
}

func validateMaxIntGate(flagName string, metricName string, actual int, rawMaximum string) error {
	if rawMaximum == "" {
		return nil
	}
	maximum, err := strconv.Atoi(rawMaximum)
	if err != nil {
		return fmt.Errorf("parse --%s value %q: %w", flagName, rawMaximum, err)
	}
	if maximum < 0 {
		return fmt.Errorf("invalid --%s value %q: must be zero or greater", flagName, rawMaximum)
	}
	if actual > maximum {
		return fmt.Errorf("acceptance gate failed: %s=%q above maximum=%q", metricName, strconv.Itoa(actual), strconv.Itoa(maximum))
	}
	return nil
}

func decimalValueFromMap(values map[string]decimal.Decimal, key string) (decimal.Decimal, bool) {
	if values == nil {
		return decimal.Zero, false
	}
	value, ok := values[key]
	return value, ok
}

func envString(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
