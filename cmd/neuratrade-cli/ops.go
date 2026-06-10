package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v2"
)

const defaultOpsTimeout = 30 * time.Minute

type opsFlagBinding struct {
	Flag   cli.Flag
	Append func(*cli.Context, []string) []string
}

type opsToolSpec struct {
	Name   string
	Usage  string
	Binary string
	Flags  []opsFlagBinding
}

type opsCommandRunner func(ctx context.Context, binary string, args []string) error

var runOpsBinary opsCommandRunner = defaultRunOpsBinary

func opsCommand() *cli.Command {
	specs := []opsToolSpec{
		{
			Name:   "scalping-soak",
			Usage:  "Run the no-order public-data scalping paper soak",
			Binary: "neuratrade-scalping-soak",
			Flags: []opsFlagBinding{
				stringOpsFlag("db", "SQLite database path for persisted soak telemetry"),
				stringOpsFlag("output", "optional path for a clean JSON result artifact"),
				stringOpsFlag("exchange", "public exchange to probe"),
				stringOpsFlag("chat-id", "chat id for persisted soak telemetry"),
				stringOpsFlag("order-prefix", "order prefix for persisted soak telemetry"),
				intOpsFlag("cycles", "number of public-data paper soak cycles"),
				intOpsFlag("interval-ms", "delay between cycles in milliseconds"),
				intOpsFlag("timeout-seconds", "overall timeout; defaults to cycles and interval"),
				intOpsFlag("hold-period-seconds", "paper position hold period in seconds; 0 uses the default"),
				intOpsFlag("max-pairs", "maximum pairs to analyze per cycle"),
				intOpsFlag("max-candidates", "maximum discovered candidates to score"),
				intOpsFlag("orderbook-pairs", "maximum pairs with orderbook quality per cycle"),
				boolOpsFlag("require-trades", "fail if the paper soak produces zero closed paper trades"),
				stringOpsFlag("capital", "initial paper capital in USDT"),
				stringOpsFlag("fee-rate", "round-trip fee-rate input used by the paper simulator"),
				boolOpsFlag("baseline", "include the broken live scalping baseline comparison"),
				intOpsFlag("min-trades", "fail unless the soak produces at least this many closed paper trades"),
				stringOpsFlag("min-win-rate", "fail unless report win_rate is at least this decimal value"),
				stringOpsFlag("min-net-pnl", "fail unless report net_pnl is at least this decimal value"),
				stringOpsFlag("min-avg-net-pnl", "fail unless avg_net_pnl_per_trade is at least this decimal value"),
				stringOpsFlag("min-signal-quality-coverage", "fail unless signal_quality.coverage is at least this decimal value"),
				stringOpsFlag("max-hold-ratio", "fail unless action_split.hold is at or below this decimal value"),
				stringOpsFlag("max-drawdown", "fail unless max_drawdown is at or below this decimal value"),
				stringOpsFlag("max-drawdown-pct", "fail unless max_drawdown_pct is at or below this decimal value"),
				stringOpsFlag("max-ai-provider-degraded-cycles", "maximum AI provider degraded cycles allowed"),
				stringOpsFlag("max-perfect-win-trades", "maximum closed trades allowed with 100% wins and zero drawdown"),
				stringOpsFlag("min-baseline-win-rate-delta", "minimum win-rate delta versus baseline"),
				stringOpsFlag("min-baseline-net-pnl-delta", "minimum net-PnL delta versus baseline"),
				stringOpsFlag("min-baseline-avg-pnl-delta", "minimum avg-PnL-per-trade delta versus baseline"),
				boolOpsFlag("require-live-trial-ready", "fail unless paper evidence is ready for live/testnet trial"),
				boolOpsFlag("record-rollout-proof", "persist live-ready paper proof metrics into rollout state"),
				stringOpsFlag("strategy-id", "strategy id for rollout proof persistence"),
			},
		},
		{
			Name:   "paper-validation",
			Usage:  "Generate paper trading evidence from stored trades",
			Binary: "neuratrade-paper-validation",
			Flags: []opsFlagBinding{
				stringOpsFlag("start", "Start time (RFC3339); defaults to 7 days ago"),
				stringOpsFlag("end", "End time (RFC3339); defaults to now"),
				stringOpsFlag("strategies", "Comma-separated strategies"),
				stringOpsFlag("capital", "Initial capital in USDT"),
				stringOpsFlag("output", "Output path for evidence JSON"),
			},
		},
		{
			Name:   "paper-readiness",
			Usage:  "Generate the paper trading readiness manifest",
			Binary: "neuratrade-paper-readiness",
			Flags: []opsFlagBinding{
				stringOpsFlag("start", "Start time (RFC3339); defaults to 7 days ago"),
				stringOpsFlag("end", "End time (RFC3339); defaults to now"),
				stringOpsFlag("strategies", "Comma-separated strategies"),
				stringOpsFlag("output", "Output path for manifest JSON"),
				boolOpsFlag("fail-if-not-ready", "Exit with non-zero code if manifest is not ready"),
			},
		},
		{
			Name:   "collect-candles",
			Usage:  "Collect historical OHLCV candles through the configured CCXT client",
			Binary: "neuratrade-collect-candles",
			Flags: []opsFlagBinding{
				stringOpsFlag("exchange", "Exchange ID (e.g. binance)"),
				stringOpsFlag("symbols", "Comma-separated trading symbols"),
				stringOpsFlag("timeframes", "Comma-separated timeframes"),
				stringOpsFlag("start", "Start time (RFC3339); defaults to 7 days ago"),
				stringOpsFlag("end", "End time (RFC3339); defaults to now"),
				intOpsFlag("limit", "Max candles per request"),
			},
		},
		{
			Name:   "seed-test-candles",
			Usage:  "Generate deterministic synthetic OHLCV candles",
			Binary: "neuratrade-seed-test-candles",
			Flags: []opsFlagBinding{
				stringOpsFlag("symbols", "Comma-separated symbols"),
				stringOpsFlag("timeframes", "Comma-separated timeframes"),
				intOpsFlag("days", "Number of days of synthetic data to generate"),
				int64OpsFlag("seed", "Random seed for reproducible data"),
			},
		},
		{
			Name:   "seed-paper-trades",
			Usage:  "Seed deterministic paper trades for readiness validation",
			Binary: "neuratrade-seed-paper-trades",
			Flags: []opsFlagBinding{
				int64OpsFlag("seed", "Random seed for reproducible trades"),
			},
		},
		{
			Name:   "backfill-paper-trades",
			Usage:  "Backfill paper trades using deterministic paper execution",
			Binary: "neuratrade-backfill-paper-trades",
			Flags: []opsFlagBinding{
				stringOpsFlag("start", "Start time (RFC3339); defaults to 30 days ago"),
				stringOpsFlag("end", "End time (RFC3339); defaults to now"),
				stringOpsFlag("capital", "Initial capital per strategy"),
			},
		},
		{
			Name:   "fetch-real-candles",
			Usage:  "Fetch recent real candles using the native CCXT service",
			Binary: "neuratrade-fetch-real-candles",
			Flags: []opsFlagBinding{
				stringOpsFlag("symbols", "Comma-separated symbols"),
				stringOpsFlag("timeframes", "Comma-separated timeframes"),
				intOpsFlag("days", "Number of days of historical data to fetch"),
			},
		},
	}

	subcommands := make([]*cli.Command, 0, len(specs))
	for _, spec := range specs {
		spec := spec
		subcommands = append(subcommands, &cli.Command{
			Name:   spec.Name,
			Usage:  spec.Usage,
			Flags:  opsCLIFlags(spec.Flags),
			Action: func(cCtx *cli.Context) error { return runOpsTool(cCtx, spec) },
		})
	}

	return &cli.Command{
		Name:        "ops",
		Usage:       "Run operational NeuraTrade tools through the main CLI",
		Subcommands: subcommands,
	}
}

func opsCLIFlags(bindings []opsFlagBinding) []cli.Flag {
	flags := make([]cli.Flag, 0, len(bindings))
	for _, binding := range bindings {
		flags = append(flags, binding.Flag)
	}
	return flags
}

func runOpsTool(cCtx *cli.Context, spec opsToolSpec) error {
	args := buildOpsArgs(cCtx, spec)
	ctx, cancel := context.WithTimeout(cCtx.Context, defaultOpsTimeout)
	defer cancel()
	if err := runOpsBinary(ctx, resolveOpsBinary(spec.Binary), args); err != nil {
		return fmt.Errorf("%s failed: %w", spec.Name, err)
	}
	return nil
}

func buildOpsArgs(cCtx *cli.Context, spec opsToolSpec) []string {
	args := make([]string, 0, len(spec.Flags))
	for _, binding := range spec.Flags {
		args = binding.Append(cCtx, args)
	}
	return args
}

func resolveOpsBinary(binary string) string {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), binary)
		if st, statErr := os.Stat(candidate); statErr == nil && !st.IsDir() {
			return candidate
		}
	}
	return binary
}

func defaultRunOpsBinary(ctx context.Context, binary string, args []string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func stringOpsFlag(name, usage string) opsFlagBinding {
	return opsFlagBinding{
		Flag: &cli.StringFlag{Name: name, Usage: usage},
		Append: func(cCtx *cli.Context, args []string) []string {
			if !cCtx.IsSet(name) {
				return args
			}
			return append(args, fmt.Sprintf("--%s=%s", name, cCtx.String(name)))
		},
	}
}

func intOpsFlag(name, usage string) opsFlagBinding {
	return opsFlagBinding{
		Flag: &cli.IntFlag{Name: name, Usage: usage},
		Append: func(cCtx *cli.Context, args []string) []string {
			if !cCtx.IsSet(name) {
				return args
			}
			return append(args, fmt.Sprintf("--%s=%d", name, cCtx.Int(name)))
		},
	}
}

func int64OpsFlag(name, usage string) opsFlagBinding {
	return opsFlagBinding{
		Flag: &cli.Int64Flag{Name: name, Usage: usage},
		Append: func(cCtx *cli.Context, args []string) []string {
			if !cCtx.IsSet(name) {
				return args
			}
			return append(args, fmt.Sprintf("--%s=%d", name, cCtx.Int64(name)))
		},
	}
}

func boolOpsFlag(name, usage string) opsFlagBinding {
	return opsFlagBinding{
		Flag: &cli.BoolFlag{Name: name, Usage: usage},
		Append: func(cCtx *cli.Context, args []string) []string {
			if !cCtx.IsSet(name) {
				return args
			}
			return append(args, fmt.Sprintf("--%s=%t", name, cCtx.Bool(name)))
		},
	}
}
