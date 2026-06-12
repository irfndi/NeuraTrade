import { Command, Options } from "@effect/cli";
import { BunContext } from "@effect/platform-bun";
import { Console, Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
import { resolve } from "node:path";
import { Path, PathLive } from "../services/path.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
} from "../market-data/repository.js";
import { defaultComposerConfig } from "../scalping/composer.js";
import type { ComposerConfig } from "../scalping/types.js";
import { runBacktest } from "../scalping/backtest.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import { runPaperTradingIteration } from "../paper-trading/engine.js";
import {
  PaperTradingRepository,
  PaperTradingRepositoryError,
  PaperTradingRepositorySQLiteLive,
} from "../paper-trading/repository.js";

const exchangeOption = Options.text("exchange").pipe(
  Options.withDefault("binance"),
  Options.withDescription("Exchange identifier"),
);

const symbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTC/USDT"),
  Options.withDescription("Trading pair symbol"),
);

const timeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("1h"),
  Options.withDescription("Candle timeframe"),
);

const capitalOption = Options.integer("capital").pipe(
  Options.withDefault(10000),
  Options.withDescription("Initial capital in quote currency"),
);

const positionSizeOption = Options.integer("position-size").pipe(
  Options.withDefault(100),
  Options.withDescription("Position size as percent of capital"),
);

const stopLossOption = Options.float("stop-loss").pipe(
  Options.withDefault(1.5),
  Options.withDescription("Stop loss percent"),
);

const takeProfitOption = Options.float("take-profit").pipe(
  Options.withDefault(3.0),
  Options.withDescription("Take profit percent"),
);

const feeOption = Options.float("fee").pipe(
  Options.withDefault(0.1),
  Options.withDescription("Trading fee percent per side"),
);

const confidenceOption = Options.float("min-confidence").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Minimum signal confidence to enter a trade"),
);

const useAtrStopsOption = Options.boolean("use-atr-stops").pipe(
  Options.withDefault(false),
  Options.withDescription("Use ATR-based dynamic stop loss and take profit"),
);

const atrStopMultiplierOption = Options.float("atr-stop-multiplier").pipe(
  Options.withDefault(1.5),
  Options.withDescription("ATR multiplier for stop loss when --use-atr-stops is set"),
);

const atrTakeProfitMultiplierOption = Options.float("atr-take-profit-multiplier").pipe(
  Options.withDefault(2.5),
  Options.withDescription("ATR multiplier for take profit when --use-atr-stops is set"),
);

const priceOnlyOption = Options.boolean("price-only").pipe(
  Options.withDefault(false),
  Options.withDescription("Ignore synthetic order-book components in backtest (trend/volatility/RSI/regime only)"),
);

const noRsiOption = Options.boolean("no-rsi").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable RSI mean-reversion component in backtest"),
);

const holdUntilStopOption = Options.boolean("hold-until-stop").pipe(
  Options.withDefault(false),
  Options.withDescription("Ignore opposite-signal exits and only exit on stop/take-profit"),
);

const noTrendOption = Options.boolean("no-trend").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable trend-following EMA component in backtest"),
);

const regimeModeOption = Options.choice("regime-mode", ["trend", "reversion"] as const).pipe(
  Options.withDefault("trend" as const),
  Options.withDescription("Regime filter mode: trend-following or mean-reversion"),
);

function makeLayer(home?: string) {
  return Layer.mergeAll(BunContext.layer, PathLive(home));
}

export const backtestCommand = Command.make(
  "backtest",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    stopLoss: stopLossOption,
    takeProfit: takeProfitOption,
    fee: feeOption,
    minConfidence: confidenceOption,
    useAtrStops: useAtrStopsOption,
    atrStopMultiplier: atrStopMultiplierOption,
    atrTakeProfitMultiplier: atrTakeProfitMultiplierOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    holdUntilStop: holdUntilStopOption,
    noTrend: noTrendOption,
    regimeMode: regimeModeOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const result = yield* backtestProgram(args).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printBacktestResult(r)),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(`backtest failed: ${err.reason}`);
            return emptyResult(args.symbol);
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Backtest deterministic scalping strategy on historical candles"));

interface BacktestArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly stopLoss: number;
  readonly takeProfit: number;
  readonly fee: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly holdUntilStop: boolean;
  readonly noTrend: boolean;
  readonly regimeMode: "trend" | "reversion";
}

function buildBacktestComposerConfig(
  priceOnly: boolean,
  noRsi: boolean,
  noTrend: boolean,
  regimeMode: "trend" | "reversion" = "trend",
): ComposerConfig {
  if (!priceOnly && !noRsi && !noTrend && regimeMode === defaultComposerConfig.thresholds.regimeMode) {
    return defaultComposerConfig;
  }

  const weights = { ...defaultComposerConfig.weights };
  if (priceOnly) {
    weights.spread = 0;
    weights.imbalance = 0;
    weights.liquidity = 0;
  }
  if (noRsi) {
    weights.rsi = 0;
  }
  if (noTrend) {
    weights.trend = 0;
  }

  const activeSum = Object.values(weights).reduce((a, b) => a + b, 0);
  if (activeSum <= 0) return defaultComposerConfig;

  const normalized: ComposerConfig["weights"] = {
    spread: weights.spread / activeSum,
    imbalance: weights.imbalance / activeSum,
    volatility: weights.volatility / activeSum,
    trend: weights.trend / activeSum,
    liquidity: weights.liquidity / activeSum,
    rsi: weights.rsi / activeSum,
    regime: weights.regime / activeSum,
  };

  return {
    weights: normalized,
    thresholds: { ...defaultComposerConfig.thresholds, regimeMode },
  };
}

function backtestProgram(args: BacktestArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;

    const candles = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
    });

    if (candles.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `No candles found for ${args.exchange}:${args.symbol}:${args.timeframe}. Run 'market fetch-candles' first.`,
        ),
      );
    }

    const composerConfig = buildBacktestComposerConfig(args.priceOnly, args.noRsi, args.noTrend, args.regimeMode);

    return runBacktest({
      symbol: args.symbol,
      exchange: args.exchange,
      timeframe: args.timeframe,
      candles,
      composerConfig,
      initialCapital: args.capital,
      positionSizePct: args.positionSize,
      stopLossPct: args.stopLoss,
      takeProfitPct: args.takeProfit,
      feePct: args.fee,
      minConfidence: args.minConfidence,
      useAtrStops: args.useAtrStops,
      atrStopMultiplier: args.atrStopMultiplier,
      atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
      holdUntilStop: args.holdUntilStop,
    });
  });
}

function printBacktestResult(result: import("../scalping/backtest.js").BacktestResult) {
  return Effect.gen(function* () {
    yield* Console.log("\n📊 Backtest Results");
    yield* Console.log("===================");
    yield* Console.log(`Symbol:        ${result.symbol}`);
    yield* Console.log(`Total trades:  ${result.totalTrades}`);
    yield* Console.log(`Win rate:      ${(result.winRate * 100).toFixed(2)}%`);
    yield* Console.log(`Total return:  ${result.totalReturnPct.toFixed(2)}%`);
    yield* Console.log(`Max drawdown:  ${result.maxDrawdownPct.toFixed(2)}%`);
    yield* Console.log(`Sharpe ratio:  ${result.sharpeRatio.toFixed(3)}`);
    if (result.trades.length > 0) {
      yield* Console.log("\nLast 5 trades:");
      for (const trade of result.trades.slice(-5)) {
        yield* Console.log(
          `  ${trade.side} ${trade.entryPrice.toFixed(2)} → ${trade.exitPrice.toFixed(2)} | ` +
            `PnL ${trade.pnlPct.toFixed(2)}% | ${trade.exitReason}`,
        );
      }
    }
  });
}

function emptyResult(symbol: string): import("../scalping/backtest.js").BacktestResult {
  return {
    symbol,
    totalTrades: 0,
    winningTrades: 0,
    losingTrades: 0,
    winRate: 0,
    totalReturnPct: 0,
    maxDrawdownPct: 0,
    sharpeRatio: 0,
    trades: [],
  };
}

const atrStopMinOption = Options.float("atr-stop-min").pipe(
  Options.withDefault(1.0),
  Options.withDescription("Minimum ATR stop multiplier to test"),
);

const atrStopMaxOption = Options.float("atr-stop-max").pipe(
  Options.withDefault(3.0),
  Options.withDescription("Maximum ATR stop multiplier to test"),
);

const atrStopStepOption = Options.float("atr-stop-step").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Step size for ATR stop multiplier"),
);

const atrTpMinOption = Options.float("atr-tp-min").pipe(
  Options.withDefault(2.0),
  Options.withDescription("Minimum ATR take-profit multiplier to test"),
);

const atrTpMaxOption = Options.float("atr-tp-max").pipe(
  Options.withDefault(5.0),
  Options.withDescription("Maximum ATR take-profit multiplier to test"),
);

const atrTpStepOption = Options.float("atr-tp-step").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Step size for ATR take-profit multiplier"),
);

const confMinOption = Options.float("conf-min").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Minimum min-confidence to test"),
);

const confMaxOption = Options.float("conf-max").pipe(
  Options.withDefault(0.7),
  Options.withDescription("Maximum min-confidence to test"),
);

const confStepOption = Options.float("conf-step").pipe(
  Options.withDefault(0.1),
  Options.withDescription("Step size for min-confidence"),
);

interface OptimizeArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly fee: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly noTrend: boolean;
  readonly holdUntilStop: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly atrStopMin: number;
  readonly atrStopMax: number;
  readonly atrStopStep: number;
  readonly atrTpMin: number;
  readonly atrTpMax: number;
  readonly atrTpStep: number;
  readonly confMin: number;
  readonly confMax: number;
  readonly confStep: number;
}

export const optimizeCommand = Command.make(
  "optimize",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    fee: feeOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    atrStopMin: atrStopMinOption,
    atrStopMax: atrStopMaxOption,
    atrStopStep: atrStopStepOption,
    atrTpMin: atrTpMinOption,
    atrTpMax: atrTpMaxOption,
    atrTpStep: atrTpStepOption,
    confMin: confMinOption,
    confMax: confMaxOption,
    confStep: confStepOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const result = yield* optimizeProgram(args).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printOptimizeResult(r, args.symbol, args.timeframe)),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(`optimize failed: ${err.reason}`);
            return [];
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Grid-search ATR/confidence parameters over historical candles"));

function optimizeProgram(args: OptimizeArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;

    const candles = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
    });

    if (candles.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `No candles found for ${args.exchange}:${args.symbol}:${args.timeframe}. Run 'market fetch-candles' first.`,
        ),
      );
    }

    const composerConfig = buildBacktestComposerConfig(args.priceOnly, args.noRsi, args.noTrend, args.regimeMode);
    const results: Array<{
      readonly stopMult: number;
      readonly tpMult: number;
      readonly minConfidence: number;
      readonly totalReturnPct: number;
      readonly sharpeRatio: number;
      readonly totalTrades: number;
      readonly winRate: number;
      readonly maxDrawdownPct: number;
    }> = [];

    for (let stopMult = args.atrStopMin; stopMult <= args.atrStopMax + 1e-9; stopMult += args.atrStopStep) {
      for (let tpMult = args.atrTpMin; tpMult <= args.atrTpMax + 1e-9; tpMult += args.atrTpStep) {
        for (let conf = args.confMin; conf <= args.confMax + 1e-9; conf += args.confStep) {
          const result = runBacktest({
            symbol: args.symbol,
            exchange: args.exchange,
            timeframe: args.timeframe,
            candles,
            composerConfig,
            initialCapital: args.capital,
            positionSizePct: args.positionSize,
            stopLossPct: 1.5,
            takeProfitPct: 3.0,
            feePct: args.fee,
            minConfidence: Number(conf.toFixed(4)),
            useAtrStops: true,
            atrStopMultiplier: Number(stopMult.toFixed(4)),
            atrTakeProfitMultiplier: Number(tpMult.toFixed(4)),
            holdUntilStop: args.holdUntilStop,
          });
          results.push({
            stopMult: Number(stopMult.toFixed(4)),
            tpMult: Number(tpMult.toFixed(4)),
            minConfidence: Number(conf.toFixed(4)),
            totalReturnPct: result.totalReturnPct,
            sharpeRatio: result.sharpeRatio,
            totalTrades: result.totalTrades,
            winRate: result.winRate,
            maxDrawdownPct: result.maxDrawdownPct,
          });
        }
      }
    }

    return results;
  });
}

function printOptimizeResult(
  results: ReadonlyArray<{
    readonly stopMult: number;
    readonly tpMult: number;
    readonly minConfidence: number;
    readonly totalReturnPct: number;
    readonly sharpeRatio: number;
    readonly totalTrades: number;
    readonly winRate: number;
    readonly maxDrawdownPct: number;
  }>,
  symbol: string,
  timeframe: string,
) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No optimization results.");
      return;
    }

    const byReturn = [...results].sort((a, b) => b.totalReturnPct - a.totalReturnPct).slice(0, 5);
    const bySharpe = [...results].sort((a, b) => b.sharpeRatio - a.sharpeRatio).slice(0, 5);

    yield* Console.log(`\n🔬 Optimization results for ${symbol} ${timeframe} (${results.length} configs tested)`);
    yield* Console.log("\nTop 5 by total return:");
    for (const r of byReturn) {
      yield* Console.log(
        `  stop=${r.stopMult.toFixed(2)} tp=${r.tpMult.toFixed(2)} conf=${r.minConfidence.toFixed(2)} | ` +
          `return=${r.totalReturnPct.toFixed(2)}% sharpe=${r.sharpeRatio.toFixed(3)} trades=${r.totalTrades} win=${(r.winRate * 100).toFixed(1)}% dd=${r.maxDrawdownPct.toFixed(2)}%`,
      );
    }

    yield* Console.log("\nTop 5 by Sharpe ratio:");
    for (const r of bySharpe) {
      yield* Console.log(
        `  stop=${r.stopMult.toFixed(2)} tp=${r.tpMult.toFixed(2)} conf=${r.minConfidence.toFixed(2)} | ` +
          `return=${r.totalReturnPct.toFixed(2)}% sharpe=${r.sharpeRatio.toFixed(3)} trades=${r.totalTrades} win=${(r.winRate * 100).toFixed(1)}% dd=${r.maxDrawdownPct.toFixed(2)}%`,
      );
    }
  });
}

const minCandlesOption = Options.integer("min-candles").pipe(
  Options.withDefault(500),
  Options.withDescription("Minimum candles required for a symbol to be included in scan"),
);

const topOption = Options.integer("top").pipe(
  Options.withDefault(0),
  Options.withDescription("Limit scan to top N symbols by candle count (0 = all)"),
);

interface ScanArgs {
  readonly exchange: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly fee: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly noTrend: boolean;
  readonly holdUntilStop: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly minCandles: number;
  readonly top: number;
}

export const scanCommand = Command.make(
  "scan",
  {
    exchange: exchangeOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    fee: feeOption,
    minConfidence: confidenceOption,
    useAtrStops: useAtrStopsOption,
    atrStopMultiplier: atrStopMultiplierOption,
    atrTakeProfitMultiplier: atrTakeProfitMultiplierOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    minCandles: minCandlesOption,
    top: topOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const result = yield* scanProgram(args).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printScanResult(r)),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(`scan failed: ${err.reason}`);
            return [];
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Backtest deterministic scalping across all stored symbols"));

function scanProgram(args: ScanArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;

    const symbols = yield* repo.listSymbols(args.exchange, args.timeframe, args.minCandles);
    if (symbols.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `No symbols found for ${args.exchange}:${args.timeframe} with >= ${args.minCandles} candles.`,
        ),
      );
    }

    const selected = args.top > 0 ? symbols.slice(0, args.top) : symbols;
    const composerConfig = buildBacktestComposerConfig(args.priceOnly, args.noRsi, args.noTrend, args.regimeMode);

    const results: Array<{
      readonly symbol: string;
      readonly totalTrades: number;
      readonly winRate: number;
      readonly totalReturnPct: number;
      readonly maxDrawdownPct: number;
      readonly sharpeRatio: number;
    }> = [];

    for (const symbol of selected) {
      const candles = yield* repo.getCandles({
        exchange: args.exchange,
        symbol,
        timeframe: args.timeframe,
      });

      if (candles.length < 50) continue;

      const result = runBacktest({
        symbol,
        exchange: args.exchange,
        timeframe: args.timeframe,
        candles,
        composerConfig,
        initialCapital: args.capital,
        positionSizePct: args.positionSize,
        stopLossPct: 1.5,
        takeProfitPct: 3.0,
        feePct: args.fee,
        minConfidence: args.minConfidence,
        useAtrStops: args.useAtrStops,
        atrStopMultiplier: args.atrStopMultiplier,
        atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
        holdUntilStop: args.holdUntilStop,
      });

      results.push({
        symbol,
        totalTrades: result.totalTrades,
        winRate: result.winRate,
        totalReturnPct: result.totalReturnPct,
        maxDrawdownPct: result.maxDrawdownPct,
        sharpeRatio: result.sharpeRatio,
      });
    }

    return results;
  });
}

function printScanResult(
  results: ReadonlyArray<{
    readonly symbol: string;
    readonly totalTrades: number;
    readonly winRate: number;
    readonly totalReturnPct: number;
    readonly maxDrawdownPct: number;
    readonly sharpeRatio: number;
  }>,
) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No scan results.");
      return;
    }

    yield* Console.log("\n🔎 Multi-ticker backtest scan");
    yield* Console.log("Symbol        Trades  Win%    Return   Drawdown  Sharpe");
    yield* Console.log("---------------------------------------------------------");

    for (const r of results) {
      yield* Console.log(
        `${r.symbol.padEnd(13)} ${String(r.totalTrades).padStart(6)}  ` +
          `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
          `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
          `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
          `${r.sharpeRatio.toFixed(3)}`,
      );
    }

    const profitable = results.filter((r) => r.totalReturnPct > 0);
    const avgReturn = results.reduce((sum, r) => sum + r.totalReturnPct, 0) / results.length;
    const avgSharpe = results.reduce((sum, r) => sum + r.sharpeRatio, 0) / results.length;

    yield* Console.log("\nSummary");
    yield* Console.log(`  Symbols tested: ${results.length}`);
    yield* Console.log(`  Profitable:     ${profitable.length} (${((profitable.length / results.length) * 100).toFixed(1)}%)`);
    yield* Console.log(`  Avg return:     ${avgReturn.toFixed(2)}%`);
    yield* Console.log(`  Avg Sharpe:     ${avgSharpe.toFixed(3)}`);
  });
}

const intervalOption = Options.integer("interval").pipe(
  Options.withDefault(60),
  Options.withDescription("Seconds between paper-trading iterations"),
);

const iterationsOption = Options.integer("iterations").pipe(
  Options.withDefault(1),
  Options.withDescription("Number of iterations to run (0 = infinite)"),
);

interface PaperTradeArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly fee: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly noTrend: boolean;
  readonly holdUntilStop: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly interval: number;
  readonly iterations: number;
}

export const paperTradeCommand = Command.make(
  "paper-trade",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    fee: feeOption,
    minConfidence: confidenceOption,
    useAtrStops: useAtrStopsOption,
    atrStopMultiplier: atrStopMultiplierOption,
    atrTakeProfitMultiplier: atrTakeProfitMultiplierOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    interval: intervalOption,
    iterations: iterationsOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(db);
      const layers = Layer.mergeAll(BunContext.layer, PathLive(process.env.NEURATRADE_HOME), MarketDataGatewayLive, repoLayer, paperRepoLayer);

      const result = yield* paperTradeProgram(args).pipe(
        Effect.provide(layers),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(`paper-trade failed: ${"reason" in err ? err.reason : String(err)}`);
            return undefined;
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Run deterministic scalping paper-trading loop"));

function paperTradeProgram(args: PaperTradeArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    yield* repo.ensureTables();

    const paperRepo = yield* PaperTradingRepository;
    yield* paperRepo.ensureTables();

    const portfolio = yield* paperRepo.getPortfolio();
    const startCapital = portfolio.capital <= 0 ? args.capital : portfolio.capital;
    yield* paperRepo.setPortfolio(startCapital, Math.max(portfolio.peakCapital, startCapital));

    const composerConfig = buildBacktestComposerConfig(args.priceOnly, args.noRsi, args.noTrend, args.regimeMode);
    const options = {
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
      composerConfig,
      positionSizePct: args.positionSize,
      feePct: args.fee,
      minConfidence: args.minConfidence,
      useAtrStops: args.useAtrStops,
      atrStopMultiplier: args.atrStopMultiplier,
      atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
      holdUntilStop: args.holdUntilStop,
      initialCapital: args.capital,
    };

    let remaining = args.iterations;
    while (remaining !== 0) {
      const result = yield* runPaperTradingIteration(options);
      yield* Console.log(
        `[${new Date().toISOString()}] ${result.action.toUpperCase()} | capital=${result.capital.toFixed(2)} | ${result.note}`,
      );

      if (remaining > 0) {
        remaining -= 1;
      }

      if (remaining !== 0) {
        yield* Effect.sleep(`${args.interval} seconds`);
      }
    }

    const closedTrades = yield* paperRepo.listRecentTrades(5);
    if (closedTrades.length > 0) {
      yield* Console.log("\nRecent closed trades:");
      for (const t of closedTrades) {
        yield* Console.log(
          `  ${t.side} ${t.entryPrice.toFixed(2)} → ${t.exitPrice.toFixed(2)} | PnL ${t.pnlPct.toFixed(2)}% | ${t.exitReason}`,
        );
      }
    }
  });
}

export const scalpCommand = Command.make("scalp", {}, () =>
  Console.log("Scalping commands. Use 'scalp backtest|optimize|scan|paper-trade --help' for details."),
).pipe(
  Command.withDescription("Deterministic scalping operations"),
  Command.withSubcommands([backtestCommand, optimizeCommand, scanCommand, paperTradeCommand]),
);
