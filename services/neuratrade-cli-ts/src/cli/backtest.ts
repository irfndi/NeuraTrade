/**
 * `neuratrade backtest scalping run` command — deterministic scalping backtest.
 *
 * Mirrors the Go CLI backtest command and feeds the backend HTTP endpoint.
 */
import { Command, Options } from "@effect/cli";
import { Console, Effect } from "effect";
import { ApiClient } from "../services/api-client.ts";
import {
  runLocalBacktest,
  type LocalBacktestConfig,
} from "../services/backtest-engine.ts";
import { add, compare, subtract } from "../services/decimal.ts";

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

function parseDate(input: string): Date {
  const trimmed = input.trim();
  if (trimmed === "") {
    throw new Error("date is required");
  }
  const parsed = new Date(trimmed);
  if (Number.isNaN(parsed.getTime())) {
    throw new Error(`invalid date: ${input}`);
  }
  return parsed;
}

function parseDuration(input: string): number | null {
  const trimmed = input.trim();
  if (trimmed === "") return null;
  const match = trimmed.match(/^(\d+)([smhd])$/i);
  if (!match) return null;
  const value = parseInt(match[1], 10);
  const unit = match[2].toLowerCase();
  const multipliers: Record<string, number> = {
    s: 1000,
    m: 60_000,
    h: 3_600_000,
    d: 86_400_000,
  };
  return value * multipliers[unit];
}

function printSummaryField(
  summary: Record<string, unknown>,
  key: string,
): Effect.Effect<void, never> {
  const value = summary[key];
  if (value === undefined || value === null) return Effect.void;
  return Console.log(`  ${key}: ${String(value)}`);
}

// ---------------------------------------------------------------------------
// Backend backtest options
// ---------------------------------------------------------------------------

const startOption = Options.text("start").pipe(
  Options.withDescription(
    "Backtest start time (RFC3339, e.g. 2025-01-01T00:00:00Z)",
  ),
);

const endOption = Options.text("end").pipe(
  Options.withDescription(
    "Backtest end time (RFC3339, e.g. 2025-12-31T00:00:00Z)",
  ),
);

const symbolsOption = Options.text("symbols").pipe(
  Options.withDescription(
    "Comma-separated symbol list (default: backend scalping universe)",
  ),
  Options.withDefault(""),
);

const exchangeOption = Options.text("exchange").pipe(
  Options.withDescription("Exchange name (default: backend default)"),
  Options.withDefault(""),
);

const initialCapitalOption = Options.text("initial-capital").pipe(
  Options.withDescription(
    "Initial capital as a decimal string (default: backend default)",
  ),
  Options.withDefault(""),
);

const modeOption = Options.text("mode").pipe(
  Options.withDescription("Decision pipeline: deterministic (default) or ai"),
  Options.withDefault("deterministic"),
);

const timeoutOption = Options.text("timeout").pipe(
  Options.withDescription("HTTP request timeout (e.g. 5m, 30s)"),
  Options.withDefault("5m"),
);

const summaryOnlyOption = Options.boolean("summary-only").pipe(
  Options.withDescription(
    "Return only summary and gate_summary (omit signals/trades). Default: true.",
  ),
  Options.withDefault(true),
);

const fullOutputOption = Options.boolean("full-output").pipe(
  Options.withDescription(
    "Include full per-signal and per-trade arrays in the response (disables summary-only).",
  ),
  Options.withDefault(false),
);

// ---------------------------------------------------------------------------
// Local backtest options
// ---------------------------------------------------------------------------

const timeframeOption = Options.text("timeframe").pipe(
  Options.withDescription("Candle timeframe (e.g. 5m, 1h, 1d)"),
  Options.withDefault("1h"),
);

const feeRateOption = Options.text("fee-rate").pipe(
  Options.withDescription("Two-sided fee rate as decimal (default: 0.0005)"),
  Options.withDefault("0.0005"),
);

const slippagePctOption = Options.text("slippage-pct").pipe(
  Options.withDescription(
    "Adverse slippage per fill as decimal (default: 0.0)",
  ),
  Options.withDefault("0.0"),
);

const leverageOption = Options.integer("leverage").pipe(
  Options.withDescription("Per-trade leverage (default: 1)"),
  Options.withDefault(1),
);

const riskPctOption = Options.text("risk-pct").pipe(
  Options.withDescription(
    "Capital risked per trade as decimal (default: 0.01)",
  ),
  Options.withDefault("0.01"),
);

const stopLossPctOption = Options.text("stop-loss-pct").pipe(
  Options.withDescription("Stop-loss distance as decimal (default: 0.005)"),
  Options.withDefault("0.005"),
);

const takeProfitPctOption = Options.text("take-profit-pct").pipe(
  Options.withDescription("Take-profit distance as decimal (default: 0.015)"),
  Options.withDefault("0.015"),
);

const trailingStopPctOption = Options.text("trailing-stop-pct").pipe(
  Options.withDescription("Trailing stop distance as decimal (default: 0.004)"),
  Options.withDefault("0.004"),
);

const maxHoldHoursOption = Options.integer("max-hold-hours").pipe(
  Options.withDescription("Maximum position hold time in hours (default: 24)"),
  Options.withDefault(24),
);

const maxOpenPositionsOption = Options.integer("max-open-positions").pipe(
  Options.withDescription("Maximum concurrent positions (default: 5)"),
  Options.withDefault(5),
);

const fastEmaOption = Options.integer("fast-ema").pipe(
  Options.withDescription("Fast EMA period (default: 9)"),
  Options.withDefault(9),
);

const slowEmaOption = Options.integer("slow-ema").pipe(
  Options.withDescription("Slow EMA period (default: 21)"),
  Options.withDefault(21),
);

const rsiPeriodOption = Options.integer("rsi-period").pipe(
  Options.withDescription("RSI period (default: 14)"),
  Options.withDefault(14),
);

const rsiLongMaxOption = Options.integer("rsi-long-max").pipe(
  Options.withDescription("Max RSI for long entries (default: 70)"),
  Options.withDefault(70),
);

const rsiShortMinOption = Options.integer("rsi-short-min").pipe(
  Options.withDescription("Min RSI for short entries (default: 30)"),
  Options.withDefault(30),
);

const rsiExitLevelOption = Options.integer("rsi-exit-level").pipe(
  Options.withDescription(
    "RSI level to exit mean-reversion trades; set >100 to disable (default: 110)",
  ),
  Options.withDefault(110),
);

const trendEmaPeriodOption = Options.integer("trend-ema").pipe(
  Options.withDescription(
    "Higher timeframe trend EMA period; 0 disables trend filter (default: 100)",
  ),
  Options.withDefault(100),
);

const breakoutPeriodOption = Options.integer("breakout-period").pipe(
  Options.withDescription(
    "Donchian breakout lookback period in candles (default: 20)",
  ),
  Options.withDefault(20),
);

const volumeLookbackOption = Options.integer("volume-lookback").pipe(
  Options.withDescription("Volume SMA lookback (default: 20)"),
  Options.withDefault(20),
);

const atrPeriodOption = Options.integer("atr-period").pipe(
  Options.withDescription("ATR period (default: 14)"),
  Options.withDefault(14),
);

const atrMaxPctOption = Options.text("atr-max-pct").pipe(
  Options.withDescription(
    "Skip entries when ATR/close exceeds this percent (default: 2.0)",
  ),
  Options.withDefault("2.0"),
);

const minVolumeRatioOption = Options.text("min-volume-ratio").pipe(
  Options.withDescription(
    "Min volume / volume-sma ratio for entries (default: 1.0)",
  ),
  Options.withDefault("1.0"),
);

const adxPeriodOption = Options.integer("adx-period").pipe(
  Options.withDescription("ADX period (default: 14)"),
  Options.withDefault(14),
);

const adxMinOption = Options.integer("adx-min").pipe(
  Options.withDescription("Min ADX required for entries, 0-100 (default: 25)"),
  Options.withDefault(25),
);

const cooldownCandlesOption = Options.integer("cooldown-candles").pipe(
  Options.withDescription(
    "Candles to skip after a trade exit per symbol (default: 5)",
  ),
  Options.withDefault(5),
);

const minTrendDistancePctOption = Options.text("min-trend-distance-pct").pipe(
  Options.withDescription(
    "Min |fastEma - slowEma| / close * 100 for entries (default: 0.2)",
  ),
  Options.withDefault("0.2"),
);

const verboseOption = Options.boolean("verbose").pipe(
  Options.withDescription("Print per-trade list"),
  Options.withDefault(false),
);

const perSymbolOption = Options.boolean("per-symbol").pipe(
  Options.withDescription("Print per-symbol summary"),
  Options.withDefault(false),
);

// ---------------------------------------------------------------------------
// Command
// ---------------------------------------------------------------------------

const runCommand = Command.make(
  "run",
  {
    start: startOption,
    end: endOption,
    symbols: symbolsOption,
    exchange: exchangeOption,
    initialCapital: initialCapitalOption,
    mode: modeOption,
    timeout: timeoutOption,
    summaryOnly: summaryOnlyOption,
    fullOutput: fullOutputOption,
  },
  ({
    start,
    end,
    symbols,
    exchange,
    initialCapital,
    mode,
    timeout,
    summaryOnly,
    fullOutput,
  }) =>
    Effect.gen(function* () {
      const startTime = parseDate(start);
      const endTime = parseDate(end);
      if (startTime.getTime() >= endTime.getTime()) {
        return yield* Effect.fail(
          new Error("invalid date range: --start must be before --end"),
        );
      }

      const normalizedMode = mode.trim().toLowerCase();
      if (normalizedMode !== "deterministic") {
        return yield* Effect.fail(
          new Error(
            `invalid mode ${JSON.stringify(mode)}: only 'deterministic' is supported`,
          ),
        );
      }

      const symbolList = symbols
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s.length > 0);

      const timeoutMs = parseDuration(timeout) ?? 300_000;

      const api = yield* ApiClient;
      const result = yield* api.runScalpingBacktest(
        {
          start_time: startTime.toISOString(),
          end_time: endTime.toISOString(),
          symbols: symbolList.length > 0 ? symbolList : undefined,
          exchange: exchange || undefined,
          initial_capital: initialCapital || undefined,
          mode: normalizedMode,
          summary_only: !fullOutput && summaryOnly,
        },
        timeoutMs,
      );

      yield* Console.log(`RunID: ${result.run_id}`);
      yield* Console.log(`Status: ${result.status}`);
      yield* Console.log(`Mode: ${result.mode}`);

      const keys = Object.keys(result.summary);
      if (keys.length === 0) {
        yield* Console.log("Summary: (none returned)");
        return;
      }

      yield* Console.log("Summary:");
      yield* printSummaryField(result.summary, "total_signals");
      yield* printSummaryField(result.summary, "accepted_signals");
      yield* printSummaryField(result.summary, "rejected_signals");
      yield* printSummaryField(result.summary, "total_trades");
      yield* printSummaryField(result.summary, "winning_trades");
      yield* printSummaryField(result.summary, "losing_trades");
      yield* printSummaryField(result.summary, "win_rate");
      yield* printSummaryField(result.summary, "total_pnl");
      yield* printSummaryField(result.summary, "total_pnl_pct");
      yield* printSummaryField(result.summary, "max_drawdown_pct");
    }).pipe(
      Effect.catchAll((err) =>
        Console.log(`❌ Backtest failed: ${err.message}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(
  Command.withDescription(
    "Run a deterministic scalping backtest via the backend",
  ),
);

const localCommand = Command.make(
  "local",
  {
    start: startOption,
    end: endOption,
    symbols: symbolsOption,
    exchange: exchangeOption,
    timeframe: timeframeOption,
    initialCapital: initialCapitalOption,
    feeRate: feeRateOption,
    slippagePct: slippagePctOption,
    leverage: leverageOption,
    riskPct: riskPctOption,
    stopLossPct: stopLossPctOption,
    takeProfitPct: takeProfitPctOption,
    trailingStopPct: trailingStopPctOption,
    maxHoldHours: maxHoldHoursOption,
    maxOpenPositions: maxOpenPositionsOption,
    fastEma: fastEmaOption,
    slowEma: slowEmaOption,
    rsiPeriod: rsiPeriodOption,
    rsiLongMax: rsiLongMaxOption,
    rsiShortMin: rsiShortMinOption,
    rsiExitLevel: rsiExitLevelOption,
    volumeLookback: volumeLookbackOption,
    atrPeriod: atrPeriodOption,
    atrMaxPct: atrMaxPctOption,
    minVolumeRatio: minVolumeRatioOption,
    adxPeriod: adxPeriodOption,
    adxMin: adxMinOption,
    cooldownCandles: cooldownCandlesOption,
    minTrendDistancePct: minTrendDistancePctOption,
    trendEmaPeriod: trendEmaPeriodOption,
    breakoutPeriod: breakoutPeriodOption,
    verbose: verboseOption,
    perSymbol: perSymbolOption,
  },
  (opts) =>
    Effect.gen(function* () {
      const startTime = parseDate(opts.start);
      const endTime = parseDate(opts.end);
      if (startTime.getTime() >= endTime.getTime()) {
        return yield* Effect.fail(
          new Error("invalid date range: --start must be before --end"),
        );
      }

      const symbolList = opts.symbols
        .split(",")
        .map((s) => s.trim().toUpperCase())
        .filter((s) => s.length > 0);
      if (symbolList.length === 0) {
        return yield* Effect.fail(
          new Error("--symbols is required (comma-separated list)"),
        );
      }

      const config: LocalBacktestConfig = {
        exchange: opts.exchange.trim().toLowerCase() || "binance",
        timeframe: opts.timeframe,
        symbols: symbolList,
        start: startTime,
        end: endTime,
        initialCapital: opts.initialCapital.trim() || "10000",
        feeRate: opts.feeRate,
        slippagePct: opts.slippagePct,
        leverage: opts.leverage,
        riskPct: opts.riskPct,
        stopLossPct: opts.stopLossPct,
        takeProfitPct: opts.takeProfitPct,
        trailingStopPct: opts.trailingStopPct,
        maxHoldHours: opts.maxHoldHours,
        maxOpenPositions: opts.maxOpenPositions,
        fastEmaPeriod: opts.fastEma,
        slowEmaPeriod: opts.slowEma,
        rsiPeriod: opts.rsiPeriod,
        rsiLongMax: opts.rsiLongMax,
        rsiShortMin: opts.rsiShortMin,
        rsiExitLevel: opts.rsiExitLevel,
        volumeLookback: opts.volumeLookback,
        atrPeriod: opts.atrPeriod,
        atrMaxPct: Number(opts.atrMaxPct),
        minVolumeRatio: Number(opts.minVolumeRatio),
        adxPeriod: opts.adxPeriod,
        adxMin: opts.adxMin,
        cooldownCandles: opts.cooldownCandles,
        minTrendDistancePct: Number(opts.minTrendDistancePct),
        trendEmaPeriod: opts.trendEmaPeriod,
        breakoutPeriod: opts.breakoutPeriod,
      };

      yield* Console.log(
        `Running local deterministic backtest for ${symbolList.length} symbols (${config.timeframe})`,
      );
      yield* Console.log(
        `Range: ${startTime.toISOString()} → ${endTime.toISOString()}`,
      );

      const result = yield* runLocalBacktest(config);

      yield* Console.log("\nSummary:");
      yield* Console.log(`  initial_capital: ${result.initialCapital}`);
      yield* Console.log(`  final_capital:   ${result.finalCapital}`);
      yield* Console.log(
        `  total_pnl:       ${result.totalPnl} (${result.totalPnlPct}%)`,
      );
      yield* Console.log(`  total_trades:    ${result.totalTrades}`);
      yield* Console.log(`  winning_trades:  ${result.winningTrades}`);
      yield* Console.log(`  losing_trades:   ${result.losingTrades}`);
      yield* Console.log(`  win_rate:        ${result.winRate}%`);
      yield* Console.log(`  profit_factor:   ${result.profitFactor}`);
      yield* Console.log(`  max_drawdown:    ${result.maxDrawdownPct}%`);
      yield* Console.log(`  sharpe_ratio:    ${result.sharpeRatio}`);

      if (opts.perSymbol) {
        const grouped = new Map<
          string,
          { wins: number; losses: number; pnl: string }
        >();
        for (const t of result.trades) {
          const g = grouped.get(t.symbol) ?? { wins: 0, losses: 0, pnl: "0" };
          grouped.set(t.symbol, {
            wins: compare(t.pnl, "0") > 0 ? g.wins + 1 : g.wins,
            losses: compare(t.pnl, "0") < 0 ? g.losses + 1 : g.losses,
            pnl: add(g.pnl, t.pnl),
          });
        }
        yield* Console.log("\nPer-symbol:");
        for (const [symbol, g] of Array.from(grouped.entries()).sort((a, b) =>
          compare(b[1].pnl, a[1].pnl),
        )) {
          yield* Console.log(
            `  ${symbol}: trades=${g.wins + g.losses} wins=${g.wins} losses=${g.losses} pnl=${g.pnl}`,
          );
        }
      }

      if (opts.verbose) {
        yield* Console.log("\nTrades:");
        for (const t of result.trades.slice(0, 50)) {
          yield* Console.log(
            `  ${t.entryAt} ${t.symbol} ${t.side} entry=${t.entryPrice} exit=${t.exitPrice} pnl=${t.pnl} (${t.pnlPct}%) reason=${t.exitReason}`,
          );
        }
        if (result.trades.length > 50) {
          yield* Console.log(
            `  ... and ${result.trades.length - 50} more trades`,
          );
        }
      }
    }).pipe(
      Effect.catchAll((err) =>
        Console.log(`❌ Local backtest failed: ${err.message}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(
  Command.withDescription(
    "Run a local deterministic scalping backtest on SQLite OHLCV data",
  ),
);

export const backtestCommand = Command.make("backtest", {}, () =>
  Console.log("Use 'backtest scalping run' or 'backtest scalping local'."),
).pipe(
  Command.withDescription("Run a scalping backtest"),
  Command.withSubcommands([
    Command.make("scalping", {}, () =>
      Console.log("Use 'backtest scalping run' or 'backtest scalping local'."),
    ).pipe(
      Command.withDescription("Scalping backtest commands"),
      Command.withSubcommands([runCommand, localCommand]),
    ),
  ]),
);
