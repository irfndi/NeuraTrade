import { Command, Options } from "@effect/cli";
import { BunContext } from "@effect/platform-bun";
import { Console, Effect, Layer, Option } from "effect";
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
import { runBacktest, type BacktestResult } from "../scalping/backtest.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import { SimulatedExchangeAdapterLive } from "../exchange/adapters/simulated.js";
import { BinanceLiveExchangeAdapterLive } from "../exchange/adapters/binance-live.js";
import { SimulatedFuturesExchangeAdapterLive } from "../exchange/adapters/simulated-futures.js";
import { BitgetFuturesExchangeAdapterLive } from "../exchange/adapters/bitget-futures.js";
import type { FuturesMarginMode } from "../exchange/futures-adapter.js";
import { RiskGuardLive } from "../risk/guards.js";
import {
  KillSwitch,
  KillSwitchSQLiteLive,
} from "../risk/kill-switch.js";
import {
  CircuitBreakerSQLiteLive,
} from "../risk/circuit-breaker.js";
import {
  runPaperTradingIteration,
  type PaperTradingOptions,
} from "../paper-trading/engine.js";
import {
  runFuturesPaperTradingIteration,
  type FuturesPaperTradingOptions,
} from "../paper-trading/futures-engine.js";
import {
  BitgetClientLiveConfig,
  type BitgetProductType,
} from "../services/bitget-client.js";
import { BitgetConfigLive } from "../services/bitget-config.js";
import { RateLimiterLive } from "../services/rate-limiter.js";
import {
  PaperTradingRepository,
  PaperTradingRepositorySQLiteLive,
} from "../paper-trading/repository.js";
import {
  runSoak,
  type SoakOptions,
  type SoakSymbol,
  type IterationResult,
} from "../scalping/soak.js";

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

const futuresOption = Options.boolean("futures").pipe(
  Options.withDefault(false),
  Options.withDescription("Trade perpetual futures instead of spot"),
);

const leverageOption = Options.integer("leverage").pipe(
  Options.withDefault(3),
  Options.withDescription("Futures leverage (default 3x)"),
);

const fundingRateOption = Options.float("funding-rate-pct").pipe(
  Options.withDefault(0.01),
  Options.withDescription(
    "Per-interval funding cost in percent (default 0.01% every 8h)",
  ),
);

const slippageBpsOption = Options.float("slippage-bps").pipe(
  Options.withDefault(0),
  Options.withDescription("Slippage in basis points applied to fills"),
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
  Options.withDescription(
    "ATR multiplier for stop loss when --use-atr-stops is set",
  ),
);

const atrTakeProfitMultiplierOption = Options.float(
  "atr-take-profit-multiplier",
).pipe(
  Options.withDefault(2.5),
  Options.withDescription(
    "ATR multiplier for take profit when --use-atr-stops is set",
  ),
);

const priceOnlyOption = Options.boolean("price-only").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Ignore synthetic order-book components in backtest (trend/volatility/RSI/regime only)",
  ),
);

const noRsiOption = Options.boolean("no-rsi").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable RSI mean-reversion component in backtest"),
);

const holdUntilStopOption = Options.boolean("hold-until-stop").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Ignore opposite-signal exits and only exit on stop/take-profit",
  ),
);

const noTrendOption = Options.boolean("no-trend").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable trend-following EMA component in backtest"),
);

const regimeModeOption = Options.choice("regime-mode", [
  "trend",
  "reversion",
] as const).pipe(
  Options.withDefault("trend" as const),
  Options.withDescription(
    "Regime filter mode: trend-following or mean-reversion",
  ),
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
    futures: futuresOption,
    fundingRatePct: fundingRateOption,
    slippageBps: slippageBpsOption,
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
).pipe(
  Command.withDescription(
    "Backtest deterministic scalping strategy on historical candles",
  ),
);

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
  readonly futures: boolean;
  readonly fundingRatePct: number;
  readonly slippageBps: number;
}

function buildBacktestComposerConfig(
  priceOnly: boolean,
  noRsi: boolean,
  noTrend: boolean,
  regimeMode: "trend" | "reversion" = "trend",
): ComposerConfig {
  if (
    !priceOnly &&
    !noRsi &&
    !noTrend &&
    regimeMode === defaultComposerConfig.thresholds.regimeMode
  ) {
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

    const composerConfig = buildBacktestComposerConfig(
      args.priceOnly,
      args.noRsi,
      args.noTrend,
      args.regimeMode,
    );

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
      isFutures: args.futures,
      fundingRatePct: args.fundingRatePct,
      slippageBps: args.slippageBps,
    });
  });
}

function printBacktestResult(
  result: import("../scalping/backtest.js").BacktestResult,
) {
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

function emptyResult(
  symbol: string,
): import("../scalping/backtest.js").BacktestResult {
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
    totalFeesPaid: 0,
    totalFundingCost: 0,
    benchmarkReturnPct: 0,
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
).pipe(
  Command.withDescription(
    "Grid-search ATR/confidence parameters over historical candles",
  ),
);

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

    const composerConfig = buildBacktestComposerConfig(
      args.priceOnly,
      args.noRsi,
      args.noTrend,
      args.regimeMode,
    );
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

    for (
      let stopMult = args.atrStopMin;
      stopMult <= args.atrStopMax + 1e-9;
      stopMult += args.atrStopStep
    ) {
      for (
        let tpMult = args.atrTpMin;
        tpMult <= args.atrTpMax + 1e-9;
        tpMult += args.atrTpStep
      ) {
        for (
          let conf = args.confMin;
          conf <= args.confMax + 1e-9;
          conf += args.confStep
        ) {
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

    const byReturn = [...results]
      .sort((a, b) => b.totalReturnPct - a.totalReturnPct)
      .slice(0, 5);
    const bySharpe = [...results]
      .sort((a, b) => b.sharpeRatio - a.sharpeRatio)
      .slice(0, 5);

    yield* Console.log(
      `\n🔬 Optimization results for ${symbol} ${timeframe} (${results.length} configs tested)`,
    );
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
  Options.withDescription(
    "Minimum candles required for a symbol to be included in scan",
  ),
);

const topOption = Options.integer("top").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Limit scan to top N symbols by candle count (0 = all)",
  ),
);

const optimizeScanOption = Options.boolean("optimize").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Run a coarse per-symbol parameter grid search and report best params",
  ),
);

const minReturnOption = Options.float("min-return-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with total return below this threshold",
  ),
);

const saveWatchlistOption = Options.text("save-watchlist").pipe(
  Options.optional,
  Options.withDescription(
    "Write passing symbols to a JSON watchlist file in NEURATRADE_HOME/data",
  ),
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
  readonly optimize: boolean;
  readonly minReturnPct: Option.Option<number>;
  readonly saveWatchlist: Option.Option<string>;
  readonly watchlistPath?: string;
  readonly futures: boolean;
  readonly fundingRatePct: number;
  readonly slippageBps: number;
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
    optimize: optimizeScanOption,
    minReturnPct: minReturnOption,
    saveWatchlist: saveWatchlistOption,
    futures: futuresOption,
    fundingRatePct: fundingRateOption,
    slippageBps: slippageBpsOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const watchlistPath = Option.match(args.saveWatchlist, {
        onNone: () => undefined as string | undefined,
        onSome: (file) => resolve(path.homeDir, "data", file),
      });

      const result = yield* scanProgram({ ...args, watchlistPath }).pipe(
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
).pipe(
  Command.withDescription(
    "Backtest deterministic scalping across all stored symbols",
  ),
);

export function scanProgram(args: ScanArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const exchanges = args.exchange
      .split(",")
      .map((e) => e.trim())
      .filter((e) => e.length > 0);

    if (exchanges.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError("No exchanges provided to scan."),
      );
    }

    const composerConfig = buildBacktestComposerConfig(
      args.priceOnly,
      args.noRsi,
      args.noTrend,
      args.regimeMode,
    );

    const results: Array<ScanResult> = [];

    for (const exchange of exchanges) {
      const exchangeResults = yield* scanSingleExchange(
        repo,
        exchange,
        args,
        composerConfig,
      );
      results.push(...exchangeResults);
    }

    if (args.watchlistPath && results.length > 0) {
      const payload = JSON.stringify(
        results.map((r) => ({
          symbol: r.symbol,
          exchange: r.exchange,
          returnPct: r.totalReturnPct,
          sharpe: r.sharpeRatio,
          bestParams: r.bestParams,
        })),
        null,
        2,
      );
      yield* Effect.tryPromise({
        try: () => Bun.write(args.watchlistPath!, payload),
        catch: (err) =>
          new MarketDataRepositoryError(
            `Failed to write watchlist: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      });
      yield* Console.log(`Watchlist saved to ${args.watchlistPath}`);
    }

    return results;
  });
}

function scanSingleExchange(
  repo: import("../market-data/repository.js").MarketDataRepositoryService,
  exchange: string,
  args: ScanArgs,
  composerConfig: ComposerConfig,
) {
  return Effect.gen(function* () {
    const symbols = yield* repo.listSymbols(
      exchange,
      args.timeframe,
      args.minCandles,
    );
    if (symbols.length === 0) {
      yield* Console.warn(
        `No symbols found for ${exchange}:${args.timeframe} with >= ${args.minCandles} candles.`,
      );
      return [];
    }

    const selected = args.top > 0 ? symbols.slice(0, args.top) : symbols;
    const results: Array<ScanResult> = [];

    for (const symbol of selected) {
      const candles = yield* repo.getCandles({
        exchange,
        symbol,
        timeframe: args.timeframe,
      });

      if (candles.length < 50) continue;

      const result = args.optimize
        ? optimizeForSymbol(symbol, candles, args, exchange, composerConfig)
        : runBacktestWithParams(
            symbol,
            candles,
            args,
            exchange,
            composerConfig,
            {
              atrStopMultiplier: args.atrStopMultiplier,
              atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
              minConfidence: args.minConfidence,
            },
          );

      if (
        Option.isSome(args.minReturnPct) &&
        result.totalReturnPct < args.minReturnPct.value
      ) {
        continue;
      }

      results.push({
        symbol,
        exchange,
        totalTrades: result.totalTrades,
        winRate: result.winRate,
        totalReturnPct: result.totalReturnPct,
        maxDrawdownPct: result.maxDrawdownPct,
        sharpeRatio: result.sharpeRatio,
        bestParams: result.bestParams,
      });
    }

    return results;
  });
}

interface ScanResult {
  readonly symbol: string;
  readonly exchange: string;
  readonly totalTrades: number;
  readonly winRate: number;
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly sharpeRatio: number;
  readonly bestParams?: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  };
}

function runBacktestWithParams(
  symbol: string,
  candles: readonly import("../scalping/types.js").CandleLike[],
  args: ScanArgs,
  exchange: string,
  composerConfig: ComposerConfig,
  params: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  },
): BacktestResult & { readonly bestParams?: undefined } {
  return runBacktest({
    symbol,
    exchange,
    timeframe: args.timeframe,
    candles,
    composerConfig,
    initialCapital: args.capital,
    positionSizePct: args.positionSize,
    stopLossPct: 1.5,
    takeProfitPct: 3.0,
    feePct: args.fee,
    minConfidence: params.minConfidence,
    useAtrStops: args.useAtrStops,
    atrStopMultiplier: params.atrStopMultiplier,
    atrTakeProfitMultiplier: params.atrTakeProfitMultiplier,
    holdUntilStop: args.holdUntilStop,
    isFutures: args.futures,
    fundingRatePct: args.fundingRatePct,
    slippageBps: args.slippageBps,
  });
}

const SCAN_STOP_MULTS = [1.5, 2.0, 2.5];
const SCAN_TP_MULTS = [2.0, 3.0, 4.0];
const SCAN_CONFIDENCES = [0.4, 0.5, 0.6];

function optimizeForSymbol(
  symbol: string,
  candles: readonly import("../scalping/types.js").CandleLike[],
  args: ScanArgs,
  exchange: string,
  composerConfig: ComposerConfig,
): BacktestResult & {
  readonly bestParams: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  };
} {
  let best: BacktestResult | null = null;
  let bestParams = {
    atrStopMultiplier: args.atrStopMultiplier,
    atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
    minConfidence: args.minConfidence,
  };

  for (const stopMult of SCAN_STOP_MULTS) {
    for (const tpMult of SCAN_TP_MULTS) {
      for (const conf of SCAN_CONFIDENCES) {
        const result = runBacktestWithParams(
          symbol,
          candles,
          args,
          exchange,
          composerConfig,
          {
            atrStopMultiplier: stopMult,
            atrTakeProfitMultiplier: tpMult,
            minConfidence: conf,
          },
        );
        if (!best || result.totalReturnPct > best.totalReturnPct) {
          best = result;
          bestParams = {
            atrStopMultiplier: stopMult,
            atrTakeProfitMultiplier: tpMult,
            minConfidence: conf,
          };
        }
      }
    }
  }

  return { ...(best ?? emptyScanResult(symbol)), bestParams };
}

function emptyScanResult(symbol: string): BacktestResult {
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
    totalFeesPaid: 0,
    totalFundingCost: 0,
    benchmarkReturnPct: 0,
  };
}

function printScanResult(results: ReadonlyArray<ScanResult>) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No scan results.");
      return;
    }

    const multiExchange = new Set(results.map((r) => r.exchange)).size > 1;

    yield* Console.log("\n🔎 Multi-ticker backtest scan");
    yield* Console.log(
      multiExchange
        ? "Exchange   Symbol        Trades  Win%    Return   Drawdown  Sharpe"
        : "Symbol        Trades  Win%    Return   Drawdown  Sharpe",
    );
    yield* Console.log(
      "--------------------------------------------------------------------",
    );

    for (const r of results) {
      const row = multiExchange
        ? `${r.exchange.padEnd(10)} ${r.symbol.padEnd(13)} ${String(r.totalTrades).padStart(6)}  ` +
          `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
          `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
          `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
          `${r.sharpeRatio.toFixed(3)}`
        : `${r.symbol.padEnd(13)} ${String(r.totalTrades).padStart(6)}  ` +
          `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
          `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
          `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
          `${r.sharpeRatio.toFixed(3)}`;
      yield* Console.log(row);
    }

    const profitable = results.filter((r) => r.totalReturnPct > 0);
    const avgReturn =
      results.reduce((sum, r) => sum + r.totalReturnPct, 0) / results.length;
    const avgSharpe =
      results.reduce((sum, r) => sum + r.sharpeRatio, 0) / results.length;

    if (results.some((r) => r.bestParams)) {
      yield* Console.log("\nBest params per symbol");
      for (const r of results) {
        if (r.bestParams) {
          const prefix = multiExchange ? `${r.exchange}:${r.symbol}` : r.symbol;
          yield* Console.log(
            `  ${prefix.padEnd(25)} stop=${r.bestParams.atrStopMultiplier.toFixed(1)} ` +
              `tp=${r.bestParams.atrTakeProfitMultiplier.toFixed(1)} ` +
              `conf=${r.bestParams.minConfidence.toFixed(1)}`,
          );
        }
      }
    }

    yield* Console.log("\nSummary");
    yield* Console.log(`  Symbols tested: ${results.length}`);
    yield* Console.log(
      `  Profitable:     ${profitable.length} (${((profitable.length / results.length) * 100).toFixed(1)}%)`,
    );
    yield* Console.log(`  Avg return:     ${avgReturn.toFixed(2)}%`);
    yield* Console.log(`  Avg Sharpe:     ${avgSharpe.toFixed(3)}`);

    if (multiExchange) {
      const byExchange = new Map<string, ScanResult[]>();
      for (const r of results) {
        const list = byExchange.get(r.exchange) ?? [];
        list.push(r);
        byExchange.set(r.exchange, list);
      }

      yield* Console.log("\nPer-exchange averages");
      for (const [exchange, list] of byExchange) {
        const avg =
          list.reduce((sum, r) => sum + r.totalReturnPct, 0) / list.length;
        const sharpe =
          list.reduce((sum, r) => sum + r.sharpeRatio, 0) / list.length;
        yield* Console.log(
          `  ${exchange.padEnd(10)} n=${String(list.length).padStart(3)} avgReturn=${avg.toFixed(2)}% avgSharpe=${sharpe.toFixed(3)}`,
        );
      }

      const bySymbol = new Map<string, ScanResult[]>();
      for (const r of results) {
        const list = bySymbol.get(r.symbol) ?? [];
        list.push(r);
        bySymbol.set(r.symbol, list);
      }
      const consistent = [...bySymbol.entries()]
        .filter(([, list]) => list.every((r) => r.totalReturnPct > 0))
        .sort((a, b) => b[1].length - a[1].length);

      if (consistent.length > 0) {
        yield* Console.log("\nCross-exchange consistent symbols");
        for (const [symbol, list] of consistent.slice(0, 10)) {
          const avg =
            list.reduce((sum, r) => sum + r.totalReturnPct, 0) / list.length;
          yield* Console.log(
            `  ${symbol.padEnd(13)} profitable on ${list.length} exchange(s) avgReturn=${avg.toFixed(2)}%`,
          );
        }
      }
    }
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

const liveOption = Options.boolean("live").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Use live exchange adapter (Binance spot or Bitget futures)",
  ),
);

const apiKeyOption = Options.text("api-key").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API key (or set BINANCE_API_KEY env)"),
);

const apiSecretOption = Options.text("api-secret").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API secret (or set BINANCE_API_SECRET env)"),
);

const marginModeOption = Options.text("margin-mode").pipe(
  Options.withDefault("crossed"),
  Options.withDescription("Futures margin mode: crossed or isolated"),
);

const productTypeOption = Options.text("product-type").pipe(
  Options.withDefault("USDT-FUTURES"),
  Options.withDescription(
    "Futures product type: USDT-FUTURES, COIN-FUTURES or USDC-FUTURES",
  ),
);

const maxDrawdownOption = Options.float("max-drawdown-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max drawdown % before blocking new trades (live default 5%)",
  ),
);

const maxDailyLossOption = Options.float("max-daily-loss-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max daily loss % before blocking new trades (live default 2%)",
  ),
);

const maxPositionSizeOption = Options.float("max-position-size-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max position size % of capital per trade (live default 10%)",
  ),
);

const maxTradesPerDayOption = Options.integer("max-trades-per-day").pipe(
  Options.optional,
  Options.withDescription("Max trades per day (live default 10)"),
);

const minCapitalOption = Options.integer("min-capital").pipe(
  Options.optional,
  Options.withDescription(
    "Minimum capital required to trade (live default 100)",
  ),
);

const watchlistOption = Options.text("watchlist").pipe(
  Options.optional,
  Options.withDescription(
    "Path to a JSON watchlist in NEURATRADE_HOME/data (uses per-symbol best params)",
  ),
);

const killSwitchOption = Options.boolean("kill-switch").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Engage kill switch before starting (blocks all new trades)",
  ),
);

const disengageOption = Options.boolean("disengage").pipe(
  Options.withDefault(false),
  Options.withDescription("Disengage kill switch before starting"),
);

interface WatchlistEntry {
  readonly symbol: string;
  readonly exchange?: string;
  readonly returnPct: number;
  readonly sharpe: number;
  readonly bestParams?: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  };
}

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
  readonly live: boolean;
  readonly apiKey: string;
  readonly apiSecret: string;
  readonly futures: boolean;
  readonly leverage: number;
  readonly marginMode: string;
  readonly productType: string;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly maxDailyLossPct: Option.Option<number>;
  readonly maxPositionSizePct: Option.Option<number>;
  readonly maxTradesPerDay: Option.Option<number>;
  readonly minCapital: Option.Option<number>;
  readonly watchlist: Option.Option<string>;
  readonly killSwitch: boolean;
  readonly disengage: boolean;
  readonly entries?: readonly WatchlistEntry[];
}

type MutablePartialRiskLimits = {
  -readonly [K in keyof import("../risk/guards.js").RiskLimits]?: import("../risk/guards.js").RiskLimits[K];
};

function loadWatchlist(
  path: string,
): Effect.Effect<readonly WatchlistEntry[], MarketDataRepositoryError> {
  return Effect.tryPromise({
    try: async () => {
      const file = Bun.file(path);
      const text = await file.text();
      return JSON.parse(text) as readonly WatchlistEntry[];
    },
    catch: (err) =>
      new MarketDataRepositoryError(
        `Failed to load watchlist from ${path}: ${err instanceof Error ? err.message : String(err)}`,
        err,
      ),
  });
}

function buildRiskOverrides(args: PaperTradeArgs): MutablePartialRiskLimits {
  const overrides: MutablePartialRiskLimits = {};
  if (Option.isSome(args.maxDrawdownPct))
    overrides.maxDrawdownPct = args.maxDrawdownPct.value;
  if (Option.isSome(args.maxDailyLossPct))
    overrides.maxDailyLossPct = args.maxDailyLossPct.value;
  if (Option.isSome(args.maxPositionSizePct))
    overrides.maxPositionSizePct = args.maxPositionSizePct.value;
  if (Option.isSome(args.maxTradesPerDay))
    overrides.maxTradesPerDay = args.maxTradesPerDay.value;
  if (Option.isSome(args.minCapital))
    overrides.minCapital = args.minCapital.value;
  return overrides;
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
    live: liveOption,
    apiKey: apiKeyOption,
    apiSecret: apiSecretOption,
    futures: futuresOption,
    leverage: leverageOption,
    marginMode: marginModeOption,
    productType: productTypeOption,
    maxDrawdownPct: maxDrawdownOption,
    maxDailyLossPct: maxDailyLossOption,
    maxPositionSizePct: maxPositionSizeOption,
    maxTradesPerDay: maxTradesPerDayOption,
    minCapital: minCapitalOption,
    watchlist: watchlistOption,
    killSwitch: killSwitchOption,
    disengage: disengageOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const watchlist = yield* Option.match(args.watchlist, {
        onNone: () => Effect.succeed<readonly WatchlistEntry[]>([]),
        onSome: (file) => loadWatchlist(resolve(path.homeDir, "data", file)),
      });

      const repoLayer = MarketDataRepositorySQLiteLive(db);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(db);
      const riskGuardLayer = RiskGuardLive(args.live, buildRiskOverrides(args));
      const killSwitchLayer = KillSwitchSQLiteLive(db);
      const circuitBreakerMaxLoss = Option.getOrElse(
        args.maxDailyLossPct,
        () => 2,
      );
      const circuitBreakerLayer = CircuitBreakerSQLiteLive(
        db,
        circuitBreakerMaxLoss,
      );
      const layers = Layer.mergeAll(
        BunContext.layer,
        PathLive(process.env.NEURATRADE_HOME),
        MarketDataGatewayLive,
        repoLayer,
        paperRepoLayer,
        riskGuardLayer,
        killSwitchLayer,
        circuitBreakerLayer,
      );

      if (args.killSwitch) {
        yield* Effect.provide(
          KillSwitch.pipe(
            Effect.flatMap((ks) => ks.engage("CLI --kill-switch")),
          ),
          killSwitchLayer,
        );
      }
      if (args.disengage) {
        yield* Effect.provide(
          KillSwitch.pipe(Effect.flatMap((ks) => ks.disengage())),
          killSwitchLayer,
        );
      }

      const result = yield* paperTradeProgram({
        ...args,
        entries: watchlist,
      }).pipe(
        Effect.provide(layers),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(
              `paper-trade failed: ${"reason" in err ? err.reason : String(err)}`,
            );
            return undefined;
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription("Run deterministic scalping paper-trading loop"),
);

function parseMarginMode(value: string): FuturesMarginMode {
  return value === "isolated" ? "isolated" : "crossed";
}

function parseProductType(value: string): BitgetProductType {
  if (
    value === "USDT-FUTURES" ||
    value === "COIN-FUTURES" ||
    value === "USDC-FUTURES"
  ) {
    return value;
  }
  return "USDT-FUTURES";
}

function paperTradeProgram(args: PaperTradeArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    yield* repo.ensureTables();

    const paperRepo = yield* PaperTradingRepository;
    yield* paperRepo.ensureTables();

    const portfolio = yield* paperRepo.getPortfolio();
    const startCapital =
      portfolio.capital <= 0 ? args.capital : portfolio.capital;
    yield* paperRepo.setPortfolio(
      startCapital,
      Math.max(portfolio.peakCapital, startCapital),
    );

    const composerConfig = buildBacktestComposerConfig(
      args.priceOnly,
      args.noRsi,
      args.noTrend,
      args.regimeMode,
    );

    const entries =
      args.entries && args.entries.length > 0 ? args.entries : undefined;

    const makeSpotOptions = (
      symbol: string,
      exchange: string,
      overrides?: Partial<PaperTradingOptions>,
    ): PaperTradingOptions => ({
      exchange,
      symbol,
      timeframe: args.timeframe,
      composerConfig,
      positionSizePct: args.positionSize,
      feePct: args.fee,
      minConfidence: overrides?.minConfidence ?? args.minConfidence,
      useAtrStops: overrides?.useAtrStops ?? args.useAtrStops,
      atrStopMultiplier: overrides?.atrStopMultiplier ?? args.atrStopMultiplier,
      atrTakeProfitMultiplier:
        overrides?.atrTakeProfitMultiplier ?? args.atrTakeProfitMultiplier,
      holdUntilStop: overrides?.holdUntilStop ?? args.holdUntilStop,
      initialCapital: args.capital,
      isLive: args.live,
    });

    const marginMode = parseMarginMode(args.marginMode);
    const productType = parseProductType(args.productType);
    const makeFuturesOptions = (
      symbol: string,
      exchange: string,
      overrides?: Partial<FuturesPaperTradingOptions>,
    ): FuturesPaperTradingOptions => ({
      exchange,
      symbol,
      timeframe: args.timeframe,
      composerConfig,
      positionSizePct: args.positionSize,
      feePct: args.fee,
      minConfidence: overrides?.minConfidence ?? args.minConfidence,
      useAtrStops: overrides?.useAtrStops ?? args.useAtrStops,
      atrStopMultiplier: overrides?.atrStopMultiplier ?? args.atrStopMultiplier,
      atrTakeProfitMultiplier:
        overrides?.atrTakeProfitMultiplier ?? args.atrTakeProfitMultiplier,
      holdUntilStop: overrides?.holdUntilStop ?? args.holdUntilStop,
      initialCapital: args.capital,
      isLive: args.live,
      leverage: args.leverage,
      marginMode,
      productType,
    });

    const spotAdapterLayer = args.live
      ? BinanceLiveExchangeAdapterLive({
          apiKey: args.apiKey || process.env.BINANCE_API_KEY || "",
          apiSecret: args.apiSecret || process.env.BINANCE_API_SECRET || "",
        })
      : SimulatedExchangeAdapterLive();
    const futuresAdapterLayer = args.live
      ? Layer.provide(
          BitgetFuturesExchangeAdapterLive,
          Layer.provide(
            BitgetClientLiveConfig,
            Layer.merge(BitgetConfigLive, RateLimiterLive()),
          ),
        )
      : SimulatedFuturesExchangeAdapterLive();

    const runSpotIteration = (
      opts: PaperTradingOptions,
    ): Effect.Effect<
      import("../paper-trading/engine.js").PaperTradingIterationResult,
      never,
      never
    > =>
      runPaperTradingIteration(opts).pipe(
        Effect.provide(spotAdapterLayer),
      ) as Effect.Effect<
        import("../paper-trading/engine.js").PaperTradingIterationResult,
        never,
        never
      >;

    const runFuturesIteration = (
      opts: FuturesPaperTradingOptions,
    ): Effect.Effect<
      import("../paper-trading/futures-engine.js").FuturesPaperTradingIterationResult,
      never,
      never
    > =>
      runFuturesPaperTradingIteration(opts).pipe(
        Effect.provide(futuresAdapterLayer),
      ) as Effect.Effect<
        import("../paper-trading/futures-engine.js").FuturesPaperTradingIterationResult,
        never,
        never
      >;

    let remaining = args.iterations;
    while (remaining !== 0) {
      if (entries) {
        for (const entry of entries) {
          if (remaining === 0) break;
          const entryExchange = entry.exchange ?? args.exchange;
          const result = args.futures
            ? yield* runFuturesIteration(
                makeFuturesOptions(entry.symbol, entryExchange, {
                  minConfidence: entry.bestParams?.minConfidence,
                  atrStopMultiplier: entry.bestParams?.atrStopMultiplier,
                  atrTakeProfitMultiplier:
                    entry.bestParams?.atrTakeProfitMultiplier,
                }),
              )
            : yield* runSpotIteration(
                makeSpotOptions(entry.symbol, entryExchange, {
                  minConfidence: entry.bestParams?.minConfidence,
                  atrStopMultiplier: entry.bestParams?.atrStopMultiplier,
                  atrTakeProfitMultiplier:
                    entry.bestParams?.atrTakeProfitMultiplier,
                }),
              );
          yield* Console.log(
            `[${new Date().toISOString()}] ${entryExchange}:${entry.symbol} ${result.action.toUpperCase()} | capital=${result.capital.toFixed(2)} | ${result.note}`,
          );

          if (remaining > 0) {
            remaining -= 1;
          }

          if (remaining !== 0) {
            yield* Effect.sleep(`${args.interval} seconds`);
          }
        }
      } else {
        const result = args.futures
          ? yield* runFuturesIteration(
              makeFuturesOptions(args.symbol, args.exchange),
            )
          : yield* runSpotIteration(
              makeSpotOptions(args.symbol, args.exchange),
            );
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

interface SoakWatchlistFileEntry {
  readonly symbol: string;
  readonly exchange?: string;
  readonly productType?: "USDT-FUTURES" | "USDC-FUTURES" | "COIN-FUTURES";
  readonly leverage?: number;
  readonly marginMode?: string;
  readonly bestParams?: {
    readonly minConfidence?: number;
    readonly atrStopMultiplier?: number;
    readonly atrTakeProfitMultiplier?: number;
  };
}

function loadSoakWatchlist(
  path: string,
): Effect.Effect<readonly SoakWatchlistFileEntry[], MarketDataRepositoryError> {
  return Effect.tryPromise({
    try: async () => {
      const file = Bun.file(path);
      const text = await file.text();
      return JSON.parse(text) as readonly SoakWatchlistFileEntry[];
    },
    catch: (err) =>
      new MarketDataRepositoryError(
        `Failed to load soak watchlist from ${path}: ${err instanceof Error ? err.message : String(err)}`,
        err,
      ),
  });
}

function printSoakResult(result: import("../scalping/soak.js").SoakResult) {
  return Effect.gen(function* () {
    yield* Console.log("\n Multi-ticker soak results");
    yield* Console.log(
      "Symbol        Trades  Return   Drawdown  Win%    Sharpe",
    );
    yield* Console.log(
      "-------------------------------------------------------",
    );

    for (const r of result.perSymbolResults) {
      yield* Console.log(
        `${r.symbol.padEnd(13)} ${String(r.trades).padStart(6)}  ` +
          `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
          `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
          `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
          `${r.sharpeRatio.toFixed(3)}`,
      );
    }

    yield* Console.log(
      "-------------------------------------------------------",
    );

    const agg = result.aggregate;
    const totalSymbols = result.perSymbolResults.length;
    yield* Console.log("\nSummary");
    yield* Console.log(`  Symbols:      ${totalSymbols}`);
    yield* Console.log(
      `  Profitable:   ${agg.profitableCount} (${totalSymbols > 0 ? ((agg.profitableCount / totalSymbols) * 100).toFixed(1) : "0.0"}%)`,
    );
    yield* Console.log(`  Avg return:   ${agg.avgReturnPct.toFixed(2)}%`);
    yield* Console.log(`  Max drawdown: ${agg.maxDrawdownPct.toFixed(2)}%`);
    yield* Console.log(`  Avg Sharpe:   ${agg.avgSharpeRatio.toFixed(3)}`);
  });
}

const soakWatchlistOption = Options.text("watchlist").pipe(
  Options.withDescription("Path to a JSON watchlist in NEURATRADE_HOME/data"),
);

export const soakCommand = Command.make(
  "soak",
  {
    watchlist: soakWatchlistOption,
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
    interval: intervalOption,
    iterations: iterationsOption,
    live: liveOption,
    apiKey: apiKeyOption,
    apiSecret: apiSecretOption,
    futures: futuresOption,
    leverage: leverageOption,
    marginMode: marginModeOption,
    productType: productTypeOption,
    maxDrawdownPct: maxDrawdownOption,
    maxDailyLossPct: maxDailyLossOption,
    maxPositionSizePct: maxPositionSizeOption,
    maxTradesPerDay: maxTradesPerDayOption,
    minCapital: minCapitalOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const watchlistPath = resolve(path.homeDir, "data", args.watchlist);
      const watchlistEntries = yield* loadSoakWatchlist(watchlistPath);

      const repoLayer = MarketDataRepositorySQLiteLive(db);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(db);
      const soakRiskOverrides: MutablePartialRiskLimits = {};
      if (Option.isSome(args.maxDrawdownPct))
        soakRiskOverrides.maxDrawdownPct = args.maxDrawdownPct.value;
      if (Option.isSome(args.maxDailyLossPct))
        soakRiskOverrides.maxDailyLossPct = args.maxDailyLossPct.value;
      if (Option.isSome(args.maxPositionSizePct))
        soakRiskOverrides.maxPositionSizePct = args.maxPositionSizePct.value;
      if (Option.isSome(args.maxTradesPerDay))
        soakRiskOverrides.maxTradesPerDay = args.maxTradesPerDay.value;
      if (Option.isSome(args.minCapital))
        soakRiskOverrides.minCapital = args.minCapital.value;
      const riskGuardLayer = RiskGuardLive(args.live, soakRiskOverrides);
      const killSwitchLayer = KillSwitchSQLiteLive(db);
      const circuitBreakerMaxLoss = Option.getOrElse(
        args.maxDailyLossPct,
        () => 2,
      );
      const circuitBreakerLayer = CircuitBreakerSQLiteLive(
        db,
        circuitBreakerMaxLoss,
      );
      const layers = Layer.mergeAll(
        BunContext.layer,
        PathLive(process.env.NEURATRADE_HOME),
        MarketDataGatewayLive,
        repoLayer,
        paperRepoLayer,
        riskGuardLayer,
        killSwitchLayer,
        circuitBreakerLayer,
      );

      const spotAdapterLayer = args.live
        ? BinanceLiveExchangeAdapterLive({
            apiKey: args.apiKey || process.env.BINANCE_API_KEY || "",
            apiSecret: args.apiSecret || process.env.BINANCE_API_SECRET || "",
          })
        : SimulatedExchangeAdapterLive();
      const futuresAdapterLayer = args.live
        ? Layer.provide(
            BitgetFuturesExchangeAdapterLive,
            Layer.provide(
              BitgetClientLiveConfig,
              Layer.merge(BitgetConfigLive, RateLimiterLive()),
            ),
          )
        : SimulatedFuturesExchangeAdapterLive();

      const composerConfig = buildBacktestComposerConfig(
        args.priceOnly,
        args.noRsi,
        args.noTrend,
        args.regimeMode,
      );

      const marginModeParsed = parseMarginMode(args.marginMode);
      const productTypeParsed = parseProductType(args.productType);

      const soakWatchlist: SoakSymbol[] = watchlistEntries.map((e) => ({
        symbol: e.symbol,
        exchange: e.exchange ?? args.exchange,
        productType:
          e.productType ?? (args.futures ? productTypeParsed : undefined),
        leverage: e.leverage ?? args.leverage,
        marginMode: (e.marginMode ??
          args.marginMode) as SoakSymbol["marginMode"],
        bestParams: e.bestParams,
      }));

      const runner = (
        symbol: string,
        exchange: string,
        bestParams?: SoakSymbol["bestParams"],
      ): Effect.Effect<IterationResult, unknown, never> => {
        const entry = soakWatchlist.find((e) => e.symbol === symbol);
        const useFutures = entry?.productType !== undefined || args.futures;

        if (useFutures) {
          const opts: FuturesPaperTradingOptions = {
            exchange,
            symbol,
            timeframe: args.timeframe,
            composerConfig,
            positionSizePct: args.positionSize,
            feePct: args.fee,
            minConfidence: bestParams?.minConfidence ?? args.minConfidence,
            useAtrStops:
              bestParams?.atrStopMultiplier !== undefined
                ? true
                : args.useAtrStops,
            atrStopMultiplier:
              bestParams?.atrStopMultiplier ?? args.atrStopMultiplier,
            atrTakeProfitMultiplier:
              bestParams?.atrTakeProfitMultiplier ??
              args.atrTakeProfitMultiplier,
            holdUntilStop: args.holdUntilStop,
            initialCapital: args.capital,
            isLive: args.live,
            leverage: entry?.leverage ?? args.leverage,
            marginMode: marginModeParsed,
            productType: productTypeParsed,
          };
          return runFuturesPaperTradingIteration(opts).pipe(
            Effect.provide(futuresAdapterLayer),
            Effect.provide(layers),
            Effect.map(
              (r): IterationResult => ({
                action: r.action,
                capital: r.capital,
                note: r.note,
              }),
            ),
          ) as Effect.Effect<IterationResult, unknown, never>;
        }

        const opts: PaperTradingOptions = {
          exchange,
          symbol,
          timeframe: args.timeframe,
          composerConfig,
          positionSizePct: args.positionSize,
          feePct: args.fee,
          minConfidence: bestParams?.minConfidence ?? args.minConfidence,
          useAtrStops:
            bestParams?.atrStopMultiplier !== undefined
              ? true
              : args.useAtrStops,
          atrStopMultiplier:
            bestParams?.atrStopMultiplier ?? args.atrStopMultiplier,
          atrTakeProfitMultiplier:
            bestParams?.atrTakeProfitMultiplier ?? args.atrTakeProfitMultiplier,
          holdUntilStop: args.holdUntilStop,
          initialCapital: args.capital,
          isLive: args.live,
        };
        return runPaperTradingIteration(opts).pipe(
          Effect.provide(spotAdapterLayer),
          Effect.provide(layers),
          Effect.map(
            (r): IterationResult => ({
              action: r.action,
              capital: r.capital,
              note: r.note,
            }),
          ),
        ) as Effect.Effect<IterationResult, unknown, never>;
      };

      const soakOptions: SoakOptions = {
        watchlist: soakWatchlist,
        iterationsPerSymbol: args.iterations,
        intervalSeconds: args.interval,
        isLive: args.live,
        initialCapital: args.capital,
        positionSizePct: args.positionSize,
        feePct: args.fee,
        minConfidence: args.minConfidence,
        useAtrStops: args.useAtrStops,
        atrStopMultiplier: args.atrStopMultiplier,
        atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
        holdUntilStop: args.holdUntilStop,
        regimeMode: args.regimeMode,
        composerConfig,
        leverage: args.leverage,
        marginMode: marginModeParsed,
        productType: productTypeParsed,
      };

      const result = yield* runSoak(soakOptions, runner).pipe(
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(
              `soak failed: ${err instanceof Error ? err.message : String(err)}`,
            );
            return {
              perSymbolResults: [],
              aggregate: {
                avgReturnPct: 0,
                profitableCount: 0,
                maxDrawdownPct: 0,
                avgSharpeRatio: 0,
                totalTrades: 0,
              },
            };
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      yield* printSoakResult(result);
      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Run multi-ticker paper-trading soak harness"));

export const scalpCommand = Command.make("scalp", {}, () =>
  Console.log(
    "Scalping commands. Use 'scalp backtest|optimize|scan|paper-trade|soak --help' for details.",
  ),
).pipe(
  Command.withDescription("Deterministic scalping operations"),
  Command.withSubcommands([
    backtestCommand,
    optimizeCommand,
    scanCommand,
    paperTradeCommand,
    soakCommand,
  ]),
);
