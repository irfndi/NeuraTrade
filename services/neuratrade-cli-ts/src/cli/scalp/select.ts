// Select / validate / strategy-library-selection helper functions.
// Extracted from the scalp.ts god-module; behavior unchanged.
//
// NOTE on the two ../scalp.js imports: buildBacktestComposerConfig still
// lives in the (in-progress) backtest cluster of scalp.ts, and
// OptimizeCandidateParams is declared next to optimizeCommand. scalp.ts
// imports this module's commands/functions in return; the cycle is safe
// because this module's top level evaluates only declarations — every
// imported binding is touched lazily inside function bodies.
import { Effect } from "effect";
import { resolve } from "node:path";
import { Path } from "../../services/path.js";
import { SqliteClient, type SqliteError } from "../../services/sqlite.js";
import {
  BacktestEngine,
  type BacktestEngineImpl,
} from "../../scalping/services.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
} from "../../market-data/repository.js";
import {
  runBacktest,
  type BacktestResult,
  type BacktestTrade,
} from "../../scalping/backtest.js";
import type { CandleLike, ComposerConfig } from "../../scalping/types.js";
import { applyPreset } from "../../scalping/presets.js";
import type {
  OptimizeArgs,
  OptimizeCandidateParams,
  OptimizeResult,
} from "../scalp.js";
import { buildBacktestComposerConfig } from "../scalp.js";
import {
  buildStrategyProfileFromArgs,
  type ResolvedBacktestArgs,
  type StrategyProfile,
  type StrategyProfileParams,
} from "../../scalping/strategy-profile.js";
import {
  buildComposerConfigFromTemplate,
  type StrategyTemplateName,
} from "../../scalping/strategy-library.js";
import { makeDbLayer } from "./shared.js";

// ---------------------------------------------------------------------------
// Select / validate / library / walk-forward helpers
// ---------------------------------------------------------------------------

export interface SelectArgs extends ResolvedBacktestArgs {
  readonly universe: string;
  readonly top: number;
  readonly minRobustness: number;
  readonly minReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly minTrades: number;
  readonly selectLookbackCandles: number;
  readonly selectBy: "return" | "sharpe" | "calmar";
}

export interface SelectResult {
  readonly symbol: string;
  readonly params: {
    readonly regimeMode: "trend" | "reversion" | "breakout";
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
    readonly adxMin: number;
  };
  readonly result: BacktestResult;
}

export interface SelectWatchlistEntry {
  readonly symbol: string;
  readonly timeframe: string;
  readonly profile: SelectResult["params"];
}

export interface ValidationRow {
  readonly symbol: string;
  readonly regimeMode: "trend" | "reversion" | "breakout";
  readonly isReturnPct: number;
  readonly oosReturnPct: number;
  readonly oosMaxDrawdownPct: number;
  readonly mcP95DrawdownPct: number;
  readonly mcRuinPct: number;
  readonly robustnessScore: number;
  readonly isTrades: number;
  readonly oosTrades: number;
  readonly liveReady: boolean;
  readonly entry: SelectWatchlistEntry;
}

/**
 * Candidate vector layout: [0]=stop, [1]=take-profit, [2]=minConfidence,
 * [3]=breakevenAtR, [4]=maxBarsInTrade, [5]=lossCooldownBars, [6]=adxMin,
 * [7]=minEfficiencyRatio, [8]=rsiLongMax, [9]=rsiShortMin,
 * [10]=entryOrderTypeIndex, [11]=entryLimitOffsetBps (only when length>=12).
 */
export function buildCandidate(
  useAtrStops: boolean,
  vector: readonly number[],
): OptimizeCandidateParams {
  return {
    useAtrStops,
    ...stopTakeProfitFromVector(useAtrStops, vector),
    ...coreThresholdsFromVector(vector),
    ...rsiBoundsFromVector(vector),
    ...entryOrderFromVector(vector),
  };
}

function stopTakeProfitFromVector(
  useAtrStops: boolean,
  vector: readonly number[],
): Pick<
  OptimizeCandidateParams,
  "stopMult" | "tpMult" | "stopLossPct" | "takeProfitPct"
> {
  return {
    stopMult: useAtrStops ? vector[0] : 0,
    tpMult: useAtrStops ? vector[1] : 0,
    stopLossPct: useAtrStops ? 0 : vector[0],
    takeProfitPct: useAtrStops ? 0 : vector[1],
  };
}

function coreThresholdsFromVector(
  vector: readonly number[],
): Pick<
  OptimizeCandidateParams,
  | "minConfidence"
  | "breakevenAtR"
  | "maxBarsInTrade"
  | "lossCooldownBars"
  | "adxMin"
  | "minEfficiencyRatio"
> {
  return {
    minConfidence: vector[2] ?? 0.5,
    breakevenAtR: vector[3] ?? 0,
    maxBarsInTrade: vector[4] ?? 0,
    lossCooldownBars: vector[5] ?? 0,
    adxMin: vector[6] ?? 0,
    minEfficiencyRatio: vector[7] ?? 0,
  };
}

function rsiBoundsFromVector(
  vector: readonly number[],
): Pick<OptimizeCandidateParams, "rsiLongMax" | "rsiShortMin"> {
  return {
    rsiLongMax: vector[8] ?? 0,
    rsiShortMin: vector[9] ?? 0,
  };
}

function entryOrderFromVector(
  vector: readonly number[],
): Pick<OptimizeCandidateParams, "entryOrderType" | "entryLimitOffsetBps"> {
  const hasEntryOrder = vector.length >= 12;
  const entryOrderTypeIndex = hasEntryOrder ? vector[10] : 0;
  return {
    entryOrderType: entryOrderTypeIndex < 0.5 ? "market" : "limit",
    entryLimitOffsetBps: hasEntryOrder ? vector[11] : 0,
  };
}

function randomInRange(min: number, max: number): number {
  if (min >= max) return min;
  return Number((min + Math.random() * (max - min)).toFixed(6));
}

function cartesianProduct<T>(arrays: T[][]): T[][] {
  return arrays.reduce<T[][]>(
    (acc, arr) => acc.flatMap((a) => arr.map((b) => [...a, b])),
    [[]],
  );
}

function range(min: number, max: number, step: number): number[] {
  const result: number[] = [];
  if (step <= 0 || max < min) {
    if (min === max) return [min];
    return [min];
  }
  for (let v = min; v <= max + 1e-9; v += step) {
    result.push(Number(v.toFixed(6)));
  }
  return result;
}

export function generateCandidates(
  args: OptimizeArgs,
): OptimizeCandidateParams[] {
  const useAtrStops = !args.noAtr;
  const vectors = topUpRandomVectors(
    args,
    useAtrStops,
    gridSearchVectors(args, useAtrStops),
  );
  return vectors.map((v) => buildCandidate(useAtrStops, v));
}

function gridSearchVectors(
  args: OptimizeArgs,
  useAtrStops: boolean,
): number[][] {
  const stopRange = useAtrStops
    ? range(args.atrStopMin, args.atrStopMax, args.atrStopStep)
    : range(args.stopLossMin, args.stopLossMax, args.stopLossStep);
  const tpRange = useAtrStops
    ? range(args.atrTpMin, args.atrTpMax, args.atrTpStep)
    : range(args.takeProfitMin, args.takeProfitMax, args.takeProfitStep);
  const confRange = range(args.confMin, args.confMax, args.confStep);
  const beRange = range(
    args.breakevenAtRMin,
    args.breakevenAtRMax,
    args.breakevenAtRStep,
  );
  const barsRange = range(
    args.maxBarsInTradeMin,
    args.maxBarsInTradeMax,
    args.maxBarsInTradeStep,
  );
  const cooldownRange = range(
    args.lossCooldownBarsMin,
    args.lossCooldownBarsMax,
    args.lossCooldownBarsStep,
  );
  const adxRange = range(args.adxMinMin, args.adxMinMax, args.adxMinStep);
  const erRange = range(
    args.minEfficiencyRatioMin,
    args.minEfficiencyRatioMax,
    args.minEfficiencyRatioStep,
  );
  const rsiLongRange = range(
    args.rsiLongMaxMin,
    args.rsiLongMaxMax,
    args.rsiLongMaxStep,
  );
  const rsiShortRange = range(
    args.rsiShortMinMin,
    args.rsiShortMinMax,
    args.rsiShortMinStep,
  );

  const dimensions = [
    stopRange,
    tpRange,
    confRange,
    beRange,
    barsRange,
    cooldownRange,
    adxRange,
    erRange,
    rsiLongRange,
    rsiShortRange,
  ];

  if (args.scanEntryOrders) {
    dimensions.push([0, 1]); // market, limit
    dimensions.push([0, 5, 10]); // offset bps
  }

  return cartesianProduct(dimensions);
}

function topUpRandomVectors(
  args: OptimizeArgs,
  useAtrStops: boolean,
  vectors: number[][],
): number[][] {
  if (args.randomSearch <= 0) return vectors;
  let pool = vectors;
  if (pool.length > args.randomSearch) {
    const shuffled = [...pool].sort(() => Math.random() - 0.5);
    pool = shuffled.slice(0, args.randomSearch);
  } else {
    // Sample additional random candidates within the search bounds.
    while (pool.length < args.randomSearch) {
      pool.push(randomVectorWithinBounds(args, useAtrStops));
    }
  }
  return pool;
}

function randomVectorWithinBounds(
  args: OptimizeArgs,
  useAtrStops: boolean,
): number[] {
  const randomVector = [
    randomInRange(
      useAtrStops ? args.atrStopMin : args.stopLossMin,
      useAtrStops ? args.atrStopMax : args.stopLossMax,
    ),
    randomInRange(
      useAtrStops ? args.atrTpMin : args.takeProfitMin,
      useAtrStops ? args.atrTpMax : args.takeProfitMax,
    ),
    randomInRange(args.confMin, args.confMax),
    randomInRange(args.breakevenAtRMin, args.breakevenAtRMax),
    Math.floor(randomInRange(args.maxBarsInTradeMin, args.maxBarsInTradeMax)),
    Math.floor(
      randomInRange(args.lossCooldownBarsMin, args.lossCooldownBarsMax),
    ),
    randomInRange(args.adxMinMin, args.adxMinMax),
    randomInRange(args.minEfficiencyRatioMin, args.minEfficiencyRatioMax),
    randomInRange(args.rsiLongMaxMin, args.rsiLongMaxMax),
    randomInRange(args.rsiShortMinMin, args.rsiShortMinMax),
  ];
  if (args.scanEntryOrders) {
    randomVector.push(Math.random() < 0.5 ? 0 : 1);
    randomVector.push([0, 5, 10][Math.floor(Math.random() * 3)]);
  }
  return randomVector;
}

export function objectiveValue(
  result: BacktestResult,
  selectBy: "return" | "sharpe" | "calmar",
): number {
  if (selectBy === "sharpe") return result.sharpeRatio;
  if (selectBy === "calmar") return result.metrics.calmarRatio;
  return result.totalReturnPct;
}

export function selectWinner(
  results: readonly OptimizeResult[],
  selectBy: "return" | "sharpe" | "calmar",
  minTrades: number,
  minOosTrades?: number,
): OptimizeResult | null {
  const oosThreshold = minOosTrades ?? minTrades;
  const passing = results.filter((r) => {
    if (r.oosResult) {
      return r.oosResult.totalTrades >= oosThreshold;
    }
    return r.isResult.totalTrades >= minTrades;
  });
  if (passing.length === 0) return null;
  const sorted = [...passing].sort(
    (a, b) =>
      objectiveValue(b.oosResult ?? b.isResult, selectBy) -
      objectiveValue(a.oosResult ?? a.isResult, selectBy),
  );
  return sorted[0];
}

export function buildStrategyProfileFromOptimizeResult(
  name: string,
  args: OptimizeArgs,
  winner: OptimizeResult,
): StrategyProfile {
  const p = winner.params;
  const override: Partial<StrategyProfileParams> = {
    minConfidence: p.minConfidence,
    adxMin: p.adxMin,
    breakevenAtR: p.breakevenAtR,
    maxBarsInTrade: p.maxBarsInTrade,
    lossCooldownBars: p.lossCooldownBars,
    minEfficiencyRatio: p.minEfficiencyRatio,
    rsiLongMax: p.rsiLongMax,
    rsiShortMin: p.rsiShortMin,
    entryOrderType: p.entryOrderType,
    entryLimitOffsetBps: p.entryLimitOffsetBps,
    ...(p.useAtrStops
      ? {
          atrStopMultiplier: p.stopMult,
          atrTakeProfitMultiplier: p.tpMult,
          stopLossPct: 0,
          takeProfitPct: 0,
        }
      : {
          stopLossPct: p.stopLossPct,
          takeProfitPct: p.takeProfitPct,
          atrStopMultiplier: 0,
          atrTakeProfitMultiplier: 0,
        }),
  };
  const defaults: StrategyProfileParams = {
    ...buildStrategyProfileFromArgs(name, args).defaults,
    ...override,
    useAtrStops: p.useAtrStops,
    exchange: args.exchange,
    defaultSymbol: args.symbol,
    timeframe: args.timeframe,
  };
  return {
    name,
    defaults,
    symbols: {
      [args.symbol]: override,
    },
  };
}

export interface WalkForwardWindow {
  readonly trainCandles: CandleLike[];
  readonly testCandles: CandleLike[];
}

export function generateWalkForwardWindows(
  candles: readonly CandleLike[],
  trainDays: number,
  testDays: number,
  stepDays: number,
): WalkForwardWindow[] {
  if (candles.length < 2) return [];
  const intervalMs =
    candles[1].timestamp.getTime() - candles[0].timestamp.getTime();
  const msPerDay = 24 * 60 * 60 * 1000;
  const candlesPerDay = Math.max(1, Math.round(msPerDay / intervalMs));
  const trainSize = Math.max(1, trainDays * candlesPerDay);
  const testSize = Math.max(1, testDays * candlesPerDay);
  const stepSize = Math.max(1, stepDays * candlesPerDay);

  const windows: WalkForwardWindow[] = [];
  for (
    let start = 0;
    start + trainSize + testSize <= candles.length;
    start += stepSize
  ) {
    windows.push({
      trainCandles: candles.slice(start, start + trainSize) as CandleLike[],
      testCandles: candles.slice(
        start + trainSize,
        start + trainSize + testSize,
      ) as CandleLike[],
    });
  }
  return windows;
}

export function combineWalkForwardResults(
  results: readonly BacktestResult[],
  initialCapital: number,
  symbol: string,
): BacktestResult {
  const combinedTrades: BacktestTrade[] = [];
  let capital = initialCapital;
  let peak = capital;
  let totalFeesPaid = 0;
  let totalFundingCost = 0;

  for (const r of results) {
    const windowStartCapital = capital;
    for (const t of r.trades) {
      const scale = windowStartCapital / initialCapital;
      const scaledNetPnl = t.netPnl * scale;
      capital += scaledNetPnl;
      if (capital > peak) peak = capital;
      combinedTrades.push({ ...t, pnl: t.pnl * scale, netPnl: scaledNetPnl });
      totalFeesPaid += (t.pnl - t.netPnl) * scale;
    }
    totalFeesPaid += r.totalFeesPaid * (windowStartCapital / initialCapital);
    totalFundingCost +=
      r.totalFundingCost * (windowStartCapital / initialCapital);
  }

  const totalReturnPct = ((capital - initialCapital) / initialCapital) * 100;
  const maxDrawdownPct = 0; // Simplified
  const winningTrades = combinedTrades.filter((t) => t.netPnl > 0).length;
  const losingTrades = combinedTrades.filter((t) => t.netPnl < 0).length;

  return {
    symbol,
    totalTrades: combinedTrades.length,
    winningTrades,
    losingTrades,
    winRate:
      combinedTrades.length > 0 ? winningTrades / combinedTrades.length : 0,
    totalReturnPct,
    maxDrawdownPct,
    sharpeRatio: 0,
    trades: combinedTrades,
    totalFeesPaid,
    totalFundingCost,
    benchmarkReturnPct: 0,
    robustnessScore: 0,
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
  };
}

function selectBacktestComposerConfig(
  args: SelectArgs,
  params: SelectResult["params"],
): ComposerConfig {
  return buildBacktestComposerConfig(
    args.priceOnly,
    args.noRsi,
    args.noTrend,
    params.regimeMode,
    args.volumeMinRatio,
    args.volumeLookback,
    args.minConfluence,
    args.entryCandleConfirm,
    args.momentumConfirmBars,
    args.adxMin,
  );
}

export function runSelectBacktest(
  symbol: string,
  candles: readonly CandleLike[],
  args: SelectArgs,
  exchange: string,
  params: SelectResult["params"],
): BacktestResult {
  const composerConfig = selectBacktestComposerConfig(args, params);
  const slippageBps = args.realistic
    ? args.realisticSlippageBps
    : args.slippageBps;
  return runBacktest({
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
    makerFeePct: args.makerFeePct,
    entryOrderType: args.entryOrderType,
    entryLimitOffsetBps: args.entryLimitOffsetBps,
    minConfidence: params.minConfidence,
    useAtrStops: true,
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
    volatilityTargetAnnualPct: args.volatilityTargetAnnualPct,
    holdUntilStop: args.holdUntilStop,
    isFutures: args.futures,
    fundingRatePct: args.fundingRatePct,
    slippageBps,
    trailingStopPct: args.trailingStopPct,
    trailingStopAtrMultiplier: args.trailingStopAtrMultiplier,
    minAtrPct: args.minAtrPct,
    signalPersistence: args.signalPersistence,
    lossConfidencePenalty: args.lossConfidencePenalty,
    lossConfidenceDecay: args.lossConfidenceDecay,
    htfCandles: [],
    htfTrendFastPeriod: args.htfTrendFastPeriod,
    htfTrendSlowPeriod: args.htfTrendSlowPeriod,
    entryPullbackEmaPeriod: args.entryPullbackEmaPeriod,
    entryPullbackMarginPct: args.entryPullbackMarginPct,
    minEfficiencyRatio: args.minEfficiencyRatio,
    efficiencyRatioPeriod: args.efficiencyRatioPeriod,
    rsiLongMax: args.rsiLongMax,
    rsiShortMin: args.rsiShortMin,
    bollingerLongMaxPctB: args.bollingerLongMaxPctB,
    bollingerShortMinPctB: args.bollingerShortMinPctB,
    recordEquityCurve: false,
    oosPct: 0,
    mcIterations: 0,
    leverage: args.leverage,
    breakevenAtR: args.breakevenAtR,
    maxBarsInTrade: args.maxBarsInTrade,
    lossCooldownBars: args.lossCooldownBars,
    sessionStart: args.sessionStart,
    sessionEnd: args.sessionEnd,
    autoRegimeFilter: args.autoRegimeFilter,
    autoRegimeAdxThreshold: args.autoRegimeAdxThreshold,
  });
}

export function selectBestForSymbol(
  symbol: string,
  candles: readonly CandleLike[],
  args: SelectArgs,
  exchange: string,
): SelectResult | null {
  const regimeModes: Array<"trend" | "reversion" | "breakout"> = [
    "trend",
    "reversion",
  ];
  const stopMults = [1.5, 2.0];
  const tpMults = [2.0, 2.5];
  const confs = [0.4, 0.5];
  const adxMins = [0, 20];

  let best: SelectResult | null = null;
  let bestObjective = -Infinity;

  for (const regimeMode of regimeModes) {
    for (const atrStopMultiplier of stopMults) {
      for (const atrTakeProfitMultiplier of tpMults) {
        for (const minConfidence of confs) {
          for (const adxMin of adxMins) {
            const params: SelectResult["params"] = {
              regimeMode,
              atrStopMultiplier,
              atrTakeProfitMultiplier,
              minConfidence,
              adxMin,
            };
            const result = runSelectBacktest(
              symbol,
              candles,
              args,
              exchange,
              params,
            );
            if (result.totalTrades < args.minTrades) continue;
            if (result.totalReturnPct < args.minReturnPct) continue;
            if (result.maxDrawdownPct > args.maxDrawdownPct) continue;
            const obj = objectiveValue(result, args.selectBy);
            if (obj > bestObjective) {
              bestObjective = obj;
              best = { symbol, params, result };
            }
          }
        }
      }
    }
  }

  return best;
}

export function cliDefaultArgs(): ResolvedBacktestArgs {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "1h",
    capital: 10000,
    positionSize: 100,
    riskPerTrade: 0,
    maxPositionSize: 100,
    stopLoss: 1.5,
    takeProfit: 3.0,
    fee: 0.1,
    makerFeePct: 0,
    entryOrderType: "market",
    entryLimitOffsetBps: 0,
    minConfidence: 0.5,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    atrRiskReward: 0,
    rsiPeriod: 14,
    rsiOversoldStrong: 30,
    rsiOverboughtStrong: 70,
    scaleOutAtR: 0,
    scaleOutPct: 50,
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
    volatilityTargetAnnualPct: 0,
    priceOnly: false,
    noRsi: false,
    holdUntilStop: false,
    noTrend: false,
    regimeMode: "trend",
    breakoutLookback: 20,
    breakoutVolumeMinRatio: 1.2,
    breakoutAdxMin: 20,
    fundingBiasThreshold: 0.0001,
    useFunding: false,
    futures: false,
    fundingRatePct: 0.01,
    slippageBps: 0,
    trailingStopPct: 0,
    trailingStopAtrMultiplier: 0,
    minAtrPct: 0,
    volumeMinRatio: 0,
    volumeLookback: 20,
    minConfluence: 0,
    entryCandleConfirm: false,
    signalPersistence: 0,
    momentumConfirmBars: 0,
    lossConfidencePenalty: 0,
    lossConfidenceDecay: 0,
    adxMin: 0,
    htfTimeframe: "",
    htfTrendFastPeriod: 50,
    htfTrendSlowPeriod: 100,
    htfSignalConfidence: 0,
    entryPullbackEmaPeriod: 0,
    entryPullbackMarginPct: 0.1,
    minEfficiencyRatio: 0,
    efficiencyRatioPeriod: 20,
    rsiLongMax: 0,
    rsiShortMin: 0,
    bollingerLongMaxPctB: -1,
    bollingerShortMinPctB: 2,
    trendFilterPeriod: 200,
    entryRsiLongThreshold: 10,
    entryRsiShortThreshold: 90,
    exitRsiPeriod: 0,
    exitRsiLongLevel: 0,
    exitRsiShortLevel: 0,
    recordEquityCurve: false,
    exportTrades: "",
    oosPct: 0,
    mcIterations: 0,
    leverage: 1,
    breakevenAtR: 0,
    maxBarsInTrade: 0,
    lossCooldownBars: 0,
    sessionStart: "",
    sessionEnd: "",
    autoRegimeFilter: false,
    autoRegimeAdxThreshold: 25,
    trendSignalStyle: "slope",
    trendFastPeriod: 9,
    trendSlowPeriod: 21,
    directionalOnly: false,
    rsiFollowTrend: false,
    strictAgreement: false,
    entryOnClose: false,
    observedPrice: false,
    realistic: false,
    strictRealism: false,
    realisticSlippageBps: 5,
    strategyType: "signal",
    gridStepPct: 0,
    gridMaxGrids: 0,
    gridPauseAfterLossBars: 0,
    onlyWithTrend: false,
    targetRatio: 1,
  };
}

export function extractExplicitOverrides(
  args: Partial<ResolvedBacktestArgs>,
): Partial<ResolvedBacktestArgs> {
  const defaults = cliDefaultArgs();
  const overrides: Record<
    string,
    ResolvedBacktestArgs[keyof ResolvedBacktestArgs]
  > = {};
  for (const key of Object.keys(args) as Array<keyof ResolvedBacktestArgs>) {
    const value = args[key];
    const defaultValue = defaults[key];
    if (value !== defaultValue) {
      overrides[key] = value;
    }
  }
  // Behavioral flags are always explicit so profiles preserve user intent.
  if (args.observedPrice !== undefined)
    overrides.observedPrice = args.observedPrice;
  if (args.realistic !== undefined) overrides.realistic = args.realistic;
  if (args.strictRealism !== undefined)
    overrides.strictRealism = args.strictRealism;
  return overrides as Partial<ResolvedBacktestArgs>;
}

export function loadSelectWatchlist(
  path: string,
): Effect.Effect<readonly SelectWatchlistEntry[], MarketDataRepositoryError> {
  return Effect.tryPromise({
    try: async () => {
      const file = Bun.file(path);
      const text = await file.text();
      return JSON.parse(text) as readonly SelectWatchlistEntry[];
    },
    catch: (err) =>
      new MarketDataRepositoryError(
        `Failed to load watchlist from ${path}: ${err instanceof Error ? err.message : String(err)}`,
        err,
      ),
  });
}

export function buildValidateBacktestArgs(
  entry: SelectWatchlistEntry,
  exchange: string,
): ResolvedBacktestArgs {
  const base = applyPreset("balanced");
  return {
    ...base,
    exchange,
    symbol: entry.symbol,
    timeframe: entry.timeframe,
    useAtrStops: true,
    atrStopMultiplier: entry.profile.atrStopMultiplier,
    atrTakeProfitMultiplier: entry.profile.atrTakeProfitMultiplier,
    minConfidence: entry.profile.minConfidence,
    regimeMode: entry.profile.regimeMode,
    adxMin: entry.profile.adxMin,
    oosPct: 20,
    mcIterations: 200,
    realistic: true,
  };
}

export function isLiveReady(row: ValidationRow): boolean {
  return (
    row.oosReturnPct > 0 &&
    row.oosMaxDrawdownPct <= 15 &&
    row.mcP95DrawdownPct <= 20 &&
    row.mcRuinPct <= 5 &&
    row.isTrades >= 10 &&
    row.oosTrades >= 10
  );
}

export function validateWatchlist(args: {
  watchlist: string;
  exchange: string;
}): Effect.Effect<
  readonly ValidationRow[],
  MarketDataRepositoryError | SqliteError
> {
  return Effect.gen(function* () {
    const path = yield* Path;
    const watchlistPath = resolve(
      path.homeDir,
      "watchlists",
      `${args.watchlist}.json`,
    );
    const entries = yield* loadSelectWatchlist(watchlistPath);

    const sqlite = yield* SqliteClient;
    const engine = yield* BacktestEngine;
    const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

    const rows = yield* Effect.gen(function* () {
      const repo = yield* MarketDataRepository;
      const result: ValidationRow[] = [];
      for (const entry of entries) {
        const backtestArgs = buildValidateBacktestArgs(entry, args.exchange);
        const candles = yield* repo.getCandles({
          exchange: args.exchange,
          symbol: entry.symbol,
          timeframe: entry.timeframe,
        });
        if (candles.length === 0) continue;
        const splitIndex = Math.floor(
          candles.length * (1 - backtestArgs.oosPct / 100),
        );
        const isCandles = candles.slice(0, splitIndex);
        const oosCandles = candles.slice(splitIndex);
        const isResult = yield* engine.runBacktest({
          ...backtestArgs,
          candles: isCandles,
          composerConfig: buildBacktestComposerConfig(
            backtestArgs.priceOnly,
            backtestArgs.noRsi,
            backtestArgs.noTrend,
            backtestArgs.regimeMode,
            backtestArgs.volumeMinRatio,
            backtestArgs.volumeLookback,
            backtestArgs.minConfluence,
            backtestArgs.entryCandleConfirm,
            backtestArgs.momentumConfirmBars,
            backtestArgs.adxMin,
          ),
          initialCapital: backtestArgs.capital,
          positionSizePct: backtestArgs.positionSize,
          riskPerTradePct: backtestArgs.riskPerTrade,
          maxPositionSizePct: backtestArgs.maxPositionSize,
          stopLossPct: backtestArgs.stopLoss,
          takeProfitPct: backtestArgs.takeProfit,
          feePct: backtestArgs.fee,
          recordEquityCurve: false,
          htfCandles: [],
        });
        const oosResult = yield* engine.runBacktest({
          ...backtestArgs,
          candles: oosCandles,
          composerConfig: buildBacktestComposerConfig(
            backtestArgs.priceOnly,
            backtestArgs.noRsi,
            backtestArgs.noTrend,
            backtestArgs.regimeMode,
            backtestArgs.volumeMinRatio,
            backtestArgs.volumeLookback,
            backtestArgs.minConfluence,
            backtestArgs.entryCandleConfirm,
            backtestArgs.momentumConfirmBars,
            backtestArgs.adxMin,
          ),
          initialCapital: backtestArgs.capital,
          positionSizePct: backtestArgs.positionSize,
          riskPerTradePct: backtestArgs.riskPerTrade,
          maxPositionSizePct: backtestArgs.maxPositionSize,
          stopLossPct: backtestArgs.stopLoss,
          takeProfitPct: backtestArgs.takeProfit,
          feePct: backtestArgs.fee,
          recordEquityCurve: false,
          htfCandles: [],
        });
        const mcP95DrawdownPct = oosResult.monteCarlo?.p95MaxDrawdownPct ?? 0;
        const mcRuinPct = oosResult.monteCarlo?.probabilityOfRuinPct ?? 0;
        let row: ValidationRow = {
          symbol: entry.symbol,
          regimeMode: entry.profile.regimeMode,
          isReturnPct: isResult.totalReturnPct,
          oosReturnPct: oosResult.totalReturnPct,
          oosMaxDrawdownPct: oosResult.maxDrawdownPct,
          mcP95DrawdownPct,
          mcRuinPct,
          robustnessScore: oosResult.robustnessScore,
          isTrades: isResult.totalTrades,
          oosTrades: oosResult.totalTrades,
          liveReady: false,
          entry,
        };
        row = { ...row, liveReady: isLiveReady(row) };
        result.push(row);
      }
      return result;
    }).pipe(Effect.provide(repoLayer));

    return rows;
  }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME)));
}

export function buildPaperTradeComposerConfig(args: {
  strategy: StrategyTemplateName;
  priceOnly: boolean;
  noRsi: boolean;
  noTrend: boolean;
  regimeMode: "trend" | "reversion" | "breakout";
  volumeMinRatio: number;
  volumeLookback: number;
  minConfluence: number;
  entryCandleConfirm: boolean;
  momentumConfirmBars: number;
  breakoutLookback: number;
  breakoutVolumeMinRatio: number;
  breakoutAdxMin: number;
  useFunding: boolean;
  fundingBiasThreshold: number;
  rsiPeriod: number;
  rsiOversoldStrong: number;
  rsiOverboughtStrong: number;
  trendFilterPeriod: number;
  entryRsiLongThreshold: number;
  entryRsiShortThreshold: number;
  exitRsiLongLevel: number;
  exitRsiShortLevel: number;
}): ComposerConfig {
  const base = buildComposerConfigFromTemplate(args.strategy);
  const custom = buildBacktestComposerConfig(
    args.priceOnly,
    args.noRsi,
    args.noTrend,
    args.regimeMode,
    args.volumeMinRatio,
    args.volumeLookback,
    args.minConfluence,
    args.entryCandleConfirm,
    args.momentumConfirmBars,
    0,
  );
  return {
    weights: { ...custom.weights, ...base.weights },
    thresholds: {
      ...custom.thresholds,
      ...base.thresholds,
      rsiPeriod: args.rsiPeriod,
      rsiOversoldStrong: args.rsiOversoldStrong,
      rsiOverboughtStrong: args.rsiOverboughtStrong,
      trendFilterPeriod: args.trendFilterPeriod,
      entryRsiLongThreshold: args.entryRsiLongThreshold,
      entryRsiShortThreshold: args.entryRsiShortThreshold,
      exitRsiLongThreshold: args.exitRsiLongLevel,
      exitRsiShortThreshold: args.exitRsiShortLevel,
      breakoutLookback: args.breakoutLookback,
      breakoutVolumeMinRatio: args.breakoutVolumeMinRatio,
      breakoutAdxMin: args.breakoutAdxMin,
      useFunding: args.useFunding,
      fundingBiasThreshold: args.fundingBiasThreshold,
    },
  };
}
