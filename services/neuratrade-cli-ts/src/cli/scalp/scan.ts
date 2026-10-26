// Scan cluster: multi-symbol backtest scan (scanCommand), gate filter, per-symbol
// nested optimization, table/summary printing, DB watchlist row type.
// Extracted from the scalp.ts god-module; behavior unchanged.
import { Command } from "../kit/kit.ts";
import { Console, Effect, Option } from "effect";
import { resolve } from "node:path";
import { Path } from "../../services/path.js";
import { SqliteClient } from "../../services/sqlite.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
  type MarketDataRepositoryService,
} from "../../market-data/repository.js";
import { CandleLike, ComposerConfig } from "../../scalping/types.js";
import { runBacktest, type BacktestResult } from "../../scalping/backtest.js";
import { BacktestEngine } from "../../scalping/services.js";
import {
  type StrategyProfile,
  type StrategyProfileParams,
} from "../../scalping/strategy-profile.js";
import {
  exchangeOption,
  timeframeOption,
  capitalOption,
  positionSizeOption,
  riskPerTradeOption,
  riskBasedMaxPositionSizeOption,
  stopLossOption,
  takeProfitOption,
  feeOption,
  futuresOption,
  fundingRateOption,
  slippageBpsOption,
  minAtrPctOption,
  volumeMinRatioOption,
  volumeLookbackOption,
  minConfluenceOption,
  entryCandleConfirmOption,
  momentumConfirmBarsOption,
  makerFeeOption,
  entryOrderTypeOption,
  entryLimitOffsetBpsOption,
  rsiPeriodOption,
  rsiOversoldStrongOption,
  rsiOverboughtStrongOption,
  trendFilterPeriodOption,
  entryRsiLongThresholdOption,
  entryRsiShortThresholdOption,
  exitRsiPeriodOption,
  exitRsiLongLevelOption,
  exitRsiShortLevelOption,
  observedPriceOption,
  realisticOption,
  strictRealismOption,
  realisticSlippageBpsOption,
  breakoutLookbackOption,
  breakoutVolumeMinRatioOption,
  breakoutAdxMinOption,
  fundingBiasThresholdOption,
  useFundingOption,
  strategyTypeOption,
  gridStepPctOption,
  gridMaxGridsOption,
  gridPauseAfterLossBarsOption,
  onlyWithTrendOption,
  targetRatioOption,
  chopGateAdxOption,
  maxHoldBarsOption,
  volatilityTargetAnnualPctOption,
  noAtrOption,
  scanEntryOrdersOption,
  randomSearchOption,
  walkForwardOption,
  wfTrainDaysOption,
  wfTestDaysOption,
  wfStepDaysOption,
  minTradesOption,
  minOosTradesOption,
  selectByOption,
  stopLossMinOption,
  stopLossMaxOption,
  stopLossStepOption,
  takeProfitMinOption,
  takeProfitMaxOption,
  takeProfitStepOption,
  breakevenAtRMinOption,
  breakevenAtRMaxOption,
  breakevenAtRStepOption,
  maxBarsInTradeMinOption,
  maxBarsInTradeMaxOption,
  maxBarsInTradeStepOption,
  lossCooldownBarsMinOption,
  lossCooldownBarsMaxOption,
  lossCooldownBarsStepOption,
  adxMinMinOption,
  adxMinMaxOption,
  adxMinStepOption,
  minEfficiencyRatioMinOption,
  minEfficiencyRatioMaxOption,
  minEfficiencyRatioStepOption,
  rsiLongMaxMinOption,
  rsiLongMaxMaxOption,
  rsiLongMaxStepOption,
  rsiShortMinMinOption,
  rsiShortMinMaxOption,
  rsiShortMinStepOption,
  profileOption,
  confidenceOption,
  useAtrStopsOption,
  atrStopMultiplierOption,
  atrTakeProfitMultiplierOption,
  atrRiskRewardOption,
  scaleOutAtROption,
  scaleOutPctOption,
  volatilityLookbackOption,
  volatilityLowPctOption,
  volatilityHighPctOption,
  volatilityLowFactorOption,
  volatilityHighFactorOption,
  priceOnlyOption,
  noRsiOption,
  holdUntilStopOption,
  noTrendOption,
  regimeModeOption,
  minCandlesOption,
  topOption,
  optimizeScanOption,
  minReturnOption,
  minSharpeOption,
  scanMaxDrawdownOption,
  saveWatchlistOption,
} from "../scalp-options/index.js";
import { makeDbLayer } from "./shared.js";
import { ScanArgs } from "./optimize.js";

import {
  buildBacktestComposerConfig,
  loadProfileIfNeeded,
} from "./backtest.js";

function resolveScanArgs(
  args: Partial<ScanArgs>,
  profile: Option.Option<StrategyProfile>,
): ScanArgs {
  if (Option.isNone(profile)) return args as ScanArgs;
  const defaults = profile.value.defaults;
  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] => defaults[key];

  const base: Partial<ScanArgs> = {
    minConfidence: get("minConfidence"),
    useAtrStops: get("useAtrStops"),
    atrStopMultiplier: get("atrStopMultiplier"),
    atrTakeProfitMultiplier: get("atrTakeProfitMultiplier"),
    atrRiskReward: get("atrRiskReward"),
    stopLoss: get("stopLossPct"),
    takeProfit: get("takeProfitPct"),
    scaleOutAtR: get("scaleOutAtR"),
    scaleOutPct: get("scaleOutPct"),
    volatilityLookback: get("volatilityLookback"),
    volatilityLowPct: get("volatilityLowPct"),
    volatilityHighPct: get("volatilityHighPct"),
    volatilityLowFactor: get("volatilityLowFactor"),
    volatilityHighFactor: get("volatilityHighFactor"),
    minAtrPct: get("minAtrPct"),
    holdUntilStop: get("holdUntilStop"),
    fee: get("feePct"),
    volumeMinRatio: get("volumeMinRatio"),
    volumeLookback: get("volumeLookback"),
    minConfluence: get("minConfluence"),
    entryCandleConfirm: get("entryCandleConfirm"),
    momentumConfirmBars: get("momentumConfirmBars"),
  };

  return { ...base, ...args } as ScanArgs;
}

export const scanCommand = Command.make(
  "scan",
  {
    exchange: exchangeOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    riskPerTrade: riskPerTradeOption,
    maxPositionSize: riskBasedMaxPositionSizeOption,
    fee: feeOption,
    minConfidence: confidenceOption,
    useAtrStops: useAtrStopsOption,
    atrStopMultiplier: atrStopMultiplierOption,
    atrTakeProfitMultiplier: atrTakeProfitMultiplierOption,
    atrRiskReward: atrRiskRewardOption,
    scaleOutAtR: scaleOutAtROption,
    scaleOutPct: scaleOutPctOption,
    volatilityLookback: volatilityLookbackOption,
    volatilityLowPct: volatilityLowPctOption,
    volatilityHighPct: volatilityHighPctOption,
    volatilityLowFactor: volatilityLowFactorOption,
    volatilityHighFactor: volatilityHighFactorOption,
    stopLoss: stopLossOption,
    takeProfit: takeProfitOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    minAtrPct: minAtrPctOption,
    minCandles: minCandlesOption,
    top: topOption,
    optimize: optimizeScanOption,
    minReturnPct: minReturnOption,
    minSharpe: minSharpeOption,
    maxDrawdownPct: scanMaxDrawdownOption,
    saveWatchlist: saveWatchlistOption,
    futures: futuresOption,
    fundingRatePct: fundingRateOption,
    slippageBps: slippageBpsOption,
    volumeMinRatio: volumeMinRatioOption,
    volumeLookback: volumeLookbackOption,
    minConfluence: minConfluenceOption,
    entryCandleConfirm: entryCandleConfirmOption,
    momentumConfirmBars: momentumConfirmBarsOption,
    noAtr: noAtrOption,
    scanEntryOrders: scanEntryOrdersOption,
    randomSearch: randomSearchOption,
    walkForward: walkForwardOption,
    wfTrainDays: wfTrainDaysOption,
    wfTestDays: wfTestDaysOption,
    wfStepDays: wfStepDaysOption,
    minTrades: minTradesOption,
    minOosTrades: minOosTradesOption,
    selectBy: selectByOption,
    stopLossMin: stopLossMinOption,
    stopLossMax: stopLossMaxOption,
    stopLossStep: stopLossStepOption,
    takeProfitMin: takeProfitMinOption,
    takeProfitMax: takeProfitMaxOption,
    takeProfitStep: takeProfitStepOption,
    breakevenAtRMin: breakevenAtRMinOption,
    breakevenAtRMax: breakevenAtRMaxOption,
    breakevenAtRStep: breakevenAtRStepOption,
    maxBarsInTradeMin: maxBarsInTradeMinOption,
    maxBarsInTradeMax: maxBarsInTradeMaxOption,
    maxBarsInTradeStep: maxBarsInTradeStepOption,
    lossCooldownBarsMin: lossCooldownBarsMinOption,
    lossCooldownBarsMax: lossCooldownBarsMaxOption,
    lossCooldownBarsStep: lossCooldownBarsStepOption,
    adxMinMin: adxMinMinOption,
    adxMinMax: adxMinMaxOption,
    adxMinStep: adxMinStepOption,
    minEfficiencyRatioMin: minEfficiencyRatioMinOption,
    minEfficiencyRatioMax: minEfficiencyRatioMaxOption,
    minEfficiencyRatioStep: minEfficiencyRatioStepOption,
    rsiLongMaxMin: rsiLongMaxMinOption,
    rsiLongMaxMax: rsiLongMaxMaxOption,
    rsiLongMaxStep: rsiLongMaxStepOption,
    rsiShortMinMin: rsiShortMinMinOption,
    rsiShortMinMax: rsiShortMinMaxOption,
    rsiShortMinStep: rsiShortMinStepOption,
    makerFeePct: makerFeeOption,
    entryOrderType: entryOrderTypeOption,
    entryLimitOffsetBps: entryLimitOffsetBpsOption,
    rsiPeriod: rsiPeriodOption,
    rsiOversoldStrong: rsiOversoldStrongOption,
    rsiOverboughtStrong: rsiOverboughtStrongOption,
    trendFilterPeriod: trendFilterPeriodOption,
    entryRsiLongThreshold: entryRsiLongThresholdOption,
    entryRsiShortThreshold: entryRsiShortThresholdOption,
    exitRsiPeriod: exitRsiPeriodOption,
    exitRsiLongLevel: exitRsiLongLevelOption,
    exitRsiShortLevel: exitRsiShortLevelOption,
    observedPrice: observedPriceOption,
    realistic: realisticOption,
    strictRealism: strictRealismOption,
    realisticSlippageBps: realisticSlippageBpsOption,
    breakoutLookback: breakoutLookbackOption,
    breakoutVolumeMinRatio: breakoutVolumeMinRatioOption,
    breakoutAdxMin: breakoutAdxMinOption,
    fundingBiasThreshold: fundingBiasThresholdOption,
    useFunding: useFundingOption,
    strategyType: strategyTypeOption,
    gridStepPct: gridStepPctOption,
    gridMaxGrids: gridMaxGridsOption,
    gridPauseAfterLossBars: gridPauseAfterLossBarsOption,
    onlyWithTrend: onlyWithTrendOption,
    targetRatio: targetRatioOption,
    chopGateAdx: chopGateAdxOption,
    maxHoldBars: maxHoldBarsOption,
    volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = resolveScanArgs(args, profile);

      const watchlistPath = Option.match(mergedArgs.saveWatchlist, {
        onNone: () => undefined as string | undefined,
        onSome: (file) => resolve(path.homeDir, "data", file),
      });

      const result = yield* scanProgram({ ...mergedArgs, watchlistPath }).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printScanResult(r)),
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Console.error(`scan failed: ${err.reason}`);
            return [];
          }),
        ),
      );

      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
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
      args.volumeMinRatio,
      args.volumeLookback,
      args.minConfluence,
      args.entryCandleConfirm,
      args.momentumConfirmBars,
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
  repo: import("../../market-data/repository.js").MarketDataRepositoryService,
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
      const result = yield* evaluateScanSymbol(
        repo,
        exchange,
        symbol,
        args,
        composerConfig,
      );
      if (result !== null) results.push(result);
    }

    return results;
  });
}

function passesScanGates(
  result: Pick<
    BacktestResult,
    "totalReturnPct" | "sharpeRatio" | "maxDrawdownPct"
  >,
  args: ScanArgs,
): boolean {
  if (
    Option.isSome(args.minReturnPct) &&
    result.totalReturnPct < args.minReturnPct.value
  ) {
    return false;
  }
  if (
    Option.isSome(args.minSharpe) &&
    result.sharpeRatio < args.minSharpe.value
  ) {
    return false;
  }
  if (
    Option.isSome(args.maxDrawdownPct) &&
    result.maxDrawdownPct > args.maxDrawdownPct.value
  ) {
    return false;
  }
  return true;
}

function evaluateScanSymbol(
  repo: import("../../market-data/repository.js").MarketDataRepositoryService,
  exchange: string,
  symbol: string,
  args: ScanArgs,
  composerConfig: ComposerConfig,
) {
  return Effect.gen(function* () {
    const candles = yield* repo.getCandles({
      exchange,
      symbol,
      timeframe: args.timeframe,
    });

    if (candles.length < 50) return null;

    const result = args.optimize
      ? yield* optimizeForSymbol(
          symbol,
          candles,
          args,
          exchange,
          composerConfig,
        )
      : yield* runBacktestWithParams(
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

    if (!passesScanGates(result, args)) return null;

    return {
      symbol,
      exchange,
      totalTrades: result.totalTrades,
      winRate: result.winRate,
      totalReturnPct: result.totalReturnPct,
      maxDrawdownPct: result.maxDrawdownPct,
      sharpeRatio: result.sharpeRatio,
      bestParams: result.bestParams,
    } satisfies ScanResult;
  });
}

export interface ScanResult {
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
  candles: readonly import("../../scalping/types.js").CandleLike[],
  args: ScanArgs,
  exchange: string,
  composerConfig: ComposerConfig,
  params: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  },
): Effect.Effect<
  BacktestResult & { readonly bestParams?: undefined },
  never,
  BacktestEngine
> {
  return Effect.gen(function* () {
    const engine = yield* BacktestEngine;
    return yield* engine.runBacktest({
      symbol,
      exchange,
      timeframe: args.timeframe,
      candles,
      composerConfig,
      initialCapital: args.capital,
      positionSizePct: args.positionSize,
      riskPerTradePct: args.riskPerTrade,
      maxPositionSizePct: args.maxPositionSize,
      stopLossPct: args.stopLoss,
      takeProfitPct: args.takeProfit,
      feePct: args.fee,
      minConfidence: params.minConfidence,
      useAtrStops: args.useAtrStops,
      atrStopMultiplier: params.atrStopMultiplier,
      atrTakeProfitMultiplier: params.atrTakeProfitMultiplier,
      atrRiskReward: args.atrRiskReward,
      scaleOutAtR: args.scaleOutAtR,
      scaleOutPct: args.scaleOutPct,
      volatilityLookback: args.volatilityLookback,
      volatilityLowPct: args.volatilityLowPct,
      volatilityHighPct: args.volatilityHighPct,
      volatilityLowFactor: args.volatilityLowFactor,
      volatilityHighFactor: args.volatilityHighFactor,
      holdUntilStop: args.holdUntilStop,
      minAtrPct: args.minAtrPct,
      isFutures: args.futures,
      fundingRatePct: args.fundingRatePct,
      slippageBps: args.slippageBps,
    });
  });
}

const SCAN_STOP_MULTS = [1.5, 2.0, 2.5];
const SCAN_TP_MULTS = [2.0, 3.0, 4.0];
const SCAN_CONFIDENCES = [0.4, 0.5, 0.6];

function optimizeForSymbol(
  symbol: string,
  candles: readonly import("../../scalping/types.js").CandleLike[],
  args: ScanArgs,
  exchange: string,
  composerConfig: ComposerConfig,
): Effect.Effect<
  BacktestResult & {
    readonly bestParams: {
      readonly atrStopMultiplier: number;
      readonly atrTakeProfitMultiplier: number;
      readonly minConfidence: number;
    };
  },
  never,
  BacktestEngine
> {
  return Effect.gen(function* () {
    let best: BacktestResult | null = null;
    let bestParams = {
      atrStopMultiplier: args.atrStopMultiplier,
      atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
      minConfidence: args.minConfidence,
    };

    for (const stopMult of SCAN_STOP_MULTS) {
      for (const tpMult of SCAN_TP_MULTS) {
        for (const conf of SCAN_CONFIDENCES) {
          const result = yield* runBacktestWithParams(
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
  });
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
    metrics: {
      profitFactor: 0,
      expectancy: 0,
      averageRMultiple: 0,
      sortinoRatio: 0,
      calmarRatio: 0,
      maxConsecutiveLosses: 0,
      averageTradeDurationHours: 0,
      timeInMarketPct: 0,
    },
    robustnessScore: 0,
  };
}

const SCAN_TABLE_HEADER_SINGLE =
  "Symbol        Trades  Win%    Return   Drawdown  Sharpe";
const SCAN_TABLE_HEADER_MULTI =
  "Exchange   Symbol        Trades  Win%    Return   Drawdown  Sharpe";
const SCAN_TABLE_SEPARATOR =
  "--------------------------------------------------------------------";

/** Single scan-result table row; identical layout with and without exchange. */
export function formatScanRow(r: ScanResult, multiExchange: boolean): string {
  const stats =
    `${String(r.totalTrades).padStart(6)}  ` +
    `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
    `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
    `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
    `${r.sharpeRatio.toFixed(3)}`;
  if (!multiExchange) return `${r.symbol.padEnd(13)} ${stats}`;
  return `${r.exchange.padEnd(10)} ${r.symbol.padEnd(13)} ${stats}`;
}

/** Best-params detail line, or null when this symbol has no tuned params. */
export function formatBestParamsLine(
  r: ScanResult,
  multiExchange: boolean,
): string | null {
  if (!r.bestParams) return null;
  const prefix = multiExchange ? `${r.exchange}:${r.symbol}` : r.symbol;
  return (
    `  ${prefix.padEnd(25)} stop=${r.bestParams.atrStopMultiplier.toFixed(1)} ` +
    `tp=${r.bestParams.atrTakeProfitMultiplier.toFixed(1)} ` +
    `conf=${r.bestParams.minConfidence.toFixed(1)}`
  );
}

export interface ScanSummary {
  readonly profitable: number;
  readonly avgReturn: number;
  readonly avgSharpe: number;
  readonly highSharpe: number;
  readonly lowDrawdown: number;
  readonly liveReady: number;
  readonly best: ScanResult;
  readonly worst: ScanResult;
}

/** Aggregate scan stats. Callers must pass a non-empty list. */
export function summarizeScanResults(
  results: ReadonlyArray<ScanResult>,
): ScanSummary {
  return {
    profitable: results.filter((r) => r.totalReturnPct > 0).length,
    avgReturn:
      results.reduce((sum, r) => sum + r.totalReturnPct, 0) / results.length,
    avgSharpe:
      results.reduce((sum, r) => sum + r.sharpeRatio, 0) / results.length,
    highSharpe: results.filter((r) => r.sharpeRatio > 0.5).length,
    lowDrawdown: results.filter((r) => r.maxDrawdownPct < 15).length,
    liveReady: results.filter(isLiveReadyScanResult).length,
    best: results.reduce((max, r) =>
      r.totalReturnPct > max.totalReturnPct ? r : max,
    ),
    worst: results.reduce((min, r) =>
      r.totalReturnPct < min.totalReturnPct ? r : min,
    ),
  };
}

function isLiveReadyScanResult(r: ScanResult): boolean {
  return r.totalReturnPct > 0 && r.sharpeRatio > 0.5 && r.maxDrawdownPct < 15;
}

function scanShare(count: number, total: number): string {
  return ((count / total) * 100).toFixed(1);
}

function logScanTable(
  results: ReadonlyArray<ScanResult>,
  multiExchange: boolean,
) {
  return Effect.gen(function* () {
    yield* Console.log("\n🔎 Multi-ticker backtest scan");
    yield* Console.log(
      multiExchange ? SCAN_TABLE_HEADER_MULTI : SCAN_TABLE_HEADER_SINGLE,
    );
    yield* Console.log(SCAN_TABLE_SEPARATOR);
    for (const r of results) {
      yield* Console.log(formatScanRow(r, multiExchange));
    }
  });
}

function logBestParamsSection(
  results: ReadonlyArray<ScanResult>,
  multiExchange: boolean,
) {
  return Effect.gen(function* () {
    const lines = results.flatMap((r) => {
      const line = formatBestParamsLine(r, multiExchange);
      return line === null ? [] : [line];
    });
    if (lines.length === 0) return;
    yield* Console.log("\nBest params per symbol");
    for (const line of lines) {
      yield* Console.log(line);
    }
  });
}

function logScanSummary(
  results: ReadonlyArray<ScanResult>,
  multiExchange: boolean,
) {
  const summary = summarizeScanResults(results);
  const bestPrefix = multiExchange ? `${summary.best.exchange}:` : "";
  const worstPrefix = multiExchange ? `${summary.worst.exchange}:` : "";
  return Effect.gen(function* () {
    yield* Console.log("\nSummary");
    yield* Console.log(`  Symbols tested: ${results.length}`);
    yield* Console.log(
      `  Profitable:     ${summary.profitable} (${scanShare(summary.profitable, results.length)}%)`,
    );
    yield* Console.log(
      `  Sharpe > 0.5:   ${summary.highSharpe} (${scanShare(summary.highSharpe, results.length)}%)`,
    );
    yield* Console.log(
      `  Drawdown < 15%: ${summary.lowDrawdown} (${scanShare(summary.lowDrawdown, results.length)}%)`,
    );
    yield* Console.log(
      `  Live-ready:     ${summary.liveReady} (${scanShare(summary.liveReady, results.length)}%)`,
    );
    yield* Console.log(`  Avg return:     ${summary.avgReturn.toFixed(2)}%`);
    yield* Console.log(`  Avg Sharpe:     ${summary.avgSharpe.toFixed(3)}`);
    yield* Console.log(
      `  Best:           ${bestPrefix}${summary.best.symbol} ${summary.best.totalReturnPct.toFixed(2)}% (Sharpe ${summary.best.sharpeRatio.toFixed(3)})`,
    );
    yield* Console.log(
      `  Worst:          ${worstPrefix}${summary.worst.symbol} ${summary.worst.totalReturnPct.toFixed(2)}% (Sharpe ${summary.worst.sharpeRatio.toFixed(3)})`,
    );
  });
}

interface GroupedScanResults {
  readonly byExchange: Map<string, ScanResult[]>;
  readonly bySymbol: Map<string, ScanResult[]>;
}

function groupScanResults(
  results: ReadonlyArray<ScanResult>,
): GroupedScanResults {
  const byExchange = new Map<string, ScanResult[]>();
  const bySymbol = new Map<string, ScanResult[]>();
  for (const r of results) {
    const exchangeList = byExchange.get(r.exchange) ?? [];
    exchangeList.push(r);
    byExchange.set(r.exchange, exchangeList);
    const symbolList = bySymbol.get(r.symbol) ?? [];
    symbolList.push(r);
    bySymbol.set(r.symbol, symbolList);
  }
  return { byExchange, bySymbol };
}

function logPerExchangeBreakdown(results: ReadonlyArray<ScanResult>) {
  return Effect.gen(function* () {
    const { byExchange, bySymbol } = groupScanResults(results);
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
  });
}

function printScanResult(results: ReadonlyArray<ScanResult>) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No scan results.");
      return;
    }

    const multiExchange = new Set(results.map((r) => r.exchange)).size > 1;

    yield* logScanTable(results, multiExchange);

    yield* logBestParamsSection(results, multiExchange);
    yield* logScanSummary(results, multiExchange);

    if (multiExchange) {
      yield* logPerExchangeBreakdown(results);
    }
  });
}

export interface WatchlistEntry {
  readonly symbol: string;
  readonly exchange?: string;
  readonly returnPct: number;
  /**
   * Present on file-based watchlists. DB-backed rows have no persisted Sharpe
   * (the watchlist schema has no sharpe column) and nothing downstream ranks on
   * this field, so it may be absent.
   */
  readonly sharpe?: number;
  readonly bestParams?: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  };
  readonly gridParams?: {
    readonly gridStepPct: number;
    readonly gridMaxGrids: number;
    readonly gridPauseAfterLossBars: number;
    /**
     * Validated config reproduced from a DB watchlist row (gate-scored by the
     * universe scan). Absent on file-based watchlists, where the CLI defaults
     * apply.
     */
    readonly targetRatio?: number;
    readonly chopGateAdx?: number;
    /**
     * Portfolio-selected allocation weight for this symbol. Comes from the
     * scan's portfolio selection (equal weights today). Absent on file-based
     * watchlists, where the full base position applies.
     */
    readonly allocatedWeight?: number;
    /**
     * Ladder rung count. Present only on ladder-scan survivor whitelist rows;
     * its presence routes the row to the incremental ladder paper engine
     * (single-position grid rows omit it).
     */
    readonly rungs?: number;
  };
}
