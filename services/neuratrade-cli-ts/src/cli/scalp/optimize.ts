// Optimize cluster: grid-search ATR/confidence parameters (optimizeCommand + walk-forward
// window evaluator, candidate runner, OOS aggregation, result printing).
// Extracted from the scalp.ts god-module; behavior unchanged.
import { Command } from "../kit/kit.ts";
import { Console, Effect, Option } from "effect";
import { Path } from "../../services/path.js";
import { SqliteClient } from "../../services/sqlite.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
} from "../../market-data/repository.js";
import { CandleLike, ComposerConfig } from "../../scalping/types.js";
import { runBacktest, type BacktestResult } from "../../scalping/backtest.js";
import {
  BacktestEngine,
  type BacktestEngineImpl,
} from "../../scalping/services.js";
import {
  findSymbolOverride,
  type ResolvedBacktestArgs,
  type StrategyProfile,
  type StrategyProfileParams,
} from "../../scalping/strategy-profile.js";
import {
  exchangeOption,
  symbolOption,
  timeframeOption,
  startOption,
  endOption,
  capitalOption,
  positionSizeOption,
  riskPerTradeOption,
  riskBasedMaxPositionSizeOption,
  feeOption,
  futuresOption,
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
  atrStopMinOption,
  atrStopMaxOption,
  atrStopStepOption,
  atrTpMinOption,
  atrTpMaxOption,
  atrTpStepOption,
  confMinOption,
  confMaxOption,
  confStepOption,
} from "../scalp-options/index.js";
import { makeDbLayer } from "./shared.js";
import {
  buildBacktestComposerConfig,
  loadProfileIfNeeded,
  requireBacktestCandleRange,
} from "./backtest.js";
// Walk-forward search-space helpers live in ./select.ts
import {
  generateCandidates,
  generateWalkForwardWindows,
  objectiveValue,
  type WalkForwardWindow,
} from "./select.js";

export interface OptimizeCandidateParams {
  readonly useAtrStops: boolean;
  readonly stopMult: number;
  readonly tpMult: number;
  readonly stopLossPct: number;
  readonly takeProfitPct: number;
  readonly minConfidence: number;
  readonly breakevenAtR: number;
  readonly maxBarsInTrade: number;
  readonly lossCooldownBars: number;
  readonly adxMin: number;
  readonly minEfficiencyRatio: number;
  readonly rsiLongMax: number;
  readonly rsiShortMin: number;
  readonly entryOrderType: "market" | "limit";
  readonly entryLimitOffsetBps: number;
}

export interface OptimizeResult {
  readonly params: OptimizeCandidateParams;
  readonly isResult: BacktestResult;
  readonly oosResult?: BacktestResult;
}

export interface OptimizeArgs extends ResolvedBacktestArgs {
  readonly atrStopMin: number;
  readonly atrStopMax: number;
  readonly atrStopStep: number;
  readonly atrTpMin: number;
  readonly atrTpMax: number;
  readonly atrTpStep: number;
  readonly confMin: number;
  readonly confMax: number;
  readonly confStep: number;
  readonly stopLossMin: number;
  readonly stopLossMax: number;
  readonly stopLossStep: number;
  readonly takeProfitMin: number;
  readonly takeProfitMax: number;
  readonly takeProfitStep: number;
  readonly breakevenAtRMin: number;
  readonly breakevenAtRMax: number;
  readonly breakevenAtRStep: number;
  readonly maxBarsInTradeMin: number;
  readonly maxBarsInTradeMax: number;
  readonly maxBarsInTradeStep: number;
  readonly lossCooldownBarsMin: number;
  readonly lossCooldownBarsMax: number;
  readonly lossCooldownBarsStep: number;
  readonly adxMinMin: number;
  readonly adxMinMax: number;
  readonly adxMinStep: number;
  readonly minEfficiencyRatioMin: number;
  readonly minEfficiencyRatioMax: number;
  readonly minEfficiencyRatioStep: number;
  readonly rsiLongMaxMin: number;
  readonly rsiLongMaxMax: number;
  readonly rsiLongMaxStep: number;
  readonly rsiShortMinMin: number;
  readonly rsiShortMinMax: number;
  readonly rsiShortMinStep: number;
  readonly scanEntryOrders: boolean;
  readonly randomSearch: number;
  readonly noAtr: boolean;
  readonly walkForward: boolean;
  readonly wfTrainDays: number;
  readonly wfTestDays: number;
  readonly wfStepDays: number;
  readonly minTrades: number;
  readonly minOosTrades: number;
  readonly selectBy: "return" | "sharpe" | "calmar";
}

function resolveOptimizeArgs(
  args: Partial<OptimizeArgs>,
  profile: Option.Option<StrategyProfile>,
): OptimizeArgs {
  if (Option.isNone(profile)) return args as OptimizeArgs;
  const overrides = findSymbolOverride(profile.value, args.symbol ?? "") ?? {};
  const resolved = profile.value;
  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] =>
    (overrides[key] !== undefined
      ? overrides[key]
      : resolved.defaults[key]) as StrategyProfileParams[K];

  const base: Partial<OptimizeArgs> = {
    atrRiskReward: get("atrRiskReward"),
    scaleOutAtR: get("scaleOutAtR"),
    scaleOutPct: get("scaleOutPct"),
    volatilityLookback: get("volatilityLookback"),
    volatilityLowPct: get("volatilityLowPct"),
    volatilityHighPct: get("volatilityHighPct"),
    volatilityLowFactor: get("volatilityLowFactor"),
    volatilityHighFactor: get("volatilityHighFactor"),
    volumeMinRatio: get("volumeMinRatio"),
    volumeLookback: get("volumeLookback"),
    minConfluence: get("minConfluence"),
    entryCandleConfirm: get("entryCandleConfirm"),
    momentumConfirmBars: get("momentumConfirmBars"),
  };

  return { ...base, ...args } as OptimizeArgs;
}

export const optimizeCommand = Command.make(
  "optimize",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    riskPerTrade: riskPerTradeOption,
    maxPositionSize: riskBasedMaxPositionSizeOption,
    fee: feeOption,
    futures: futuresOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    atrRiskReward: atrRiskRewardOption,
    scaleOutAtR: scaleOutAtROption,
    scaleOutPct: scaleOutPctOption,
    volatilityLookback: volatilityLookbackOption,
    volatilityLowPct: volatilityLowPctOption,
    volatilityHighPct: volatilityHighPctOption,
    volatilityLowFactor: volatilityLowFactorOption,
    volatilityHighFactor: volatilityHighFactorOption,
    atrStopMin: atrStopMinOption,
    atrStopMax: atrStopMaxOption,
    atrStopStep: atrStopStepOption,
    atrTpMin: atrTpMinOption,
    atrTpMax: atrTpMaxOption,
    atrTpStep: atrTpStepOption,
    confMin: confMinOption,
    confMax: confMaxOption,
    confStep: confStepOption,
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
    start: startOption,
    end: endOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const programArgs = resolveOptimizeArgs(args, profile);

      const result = yield* optimizeProgram(programArgs).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) =>
          printOptimizeResult(r, args.symbol, args.timeframe, args.capital),
        ),
        Effect.catch((err) =>
          Effect.gen(function* () {
            const msg =
              err instanceof Error
                ? err.message
                : String((err as { readonly reason?: unknown }).reason ?? err);
            yield* Console.error(`optimize failed: ${msg}`);
            return [];
          }),
        ),
      );

      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Grid-search ATR/confidence parameters over historical candles",
  ),
);

function optimizeProgram(args: OptimizeArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const engine = yield* BacktestEngine;

    const { from, to } = yield* requireBacktestCandleRange(args);

    const candles = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
      from,
      to,
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
      args.volumeMinRatio,
      args.volumeLookback,
      args.minConfluence,
      args.entryCandleConfirm,
      args.momentumConfirmBars,
    );
    const candidates = generateCandidates(args);
    const results: OptimizeResult[] = [];

    if (!args.walkForward) {
      for (const params of candidates) {
        const isResult = yield* runOptimizeCandidate(
          engine,
          args,
          candles,
          composerConfig,
          params,
        );
        results.push({ params, isResult });
      }
      return results;
    }

    const windows = generateWalkForwardWindows(
      candles,
      args.wfTrainDays,
      args.wfTestDays,
      args.wfStepDays,
    );
    for (const window of windows) {
      const result = yield* evaluateWalkForwardWindow(
        engine,
        args,
        window,
        composerConfig,
        candidates,
      );
      if (result !== null) results.push(result);
    }

    return results;
  });
}

function evaluateWalkForwardWindow(
  engine: BacktestEngineImpl,
  args: OptimizeArgs,
  window: WalkForwardWindow,
  composerConfig: ComposerConfig,
  candidates: readonly OptimizeCandidateParams[],
) {
  return Effect.gen(function* () {
    let selected:
      | {
          readonly params: OptimizeCandidateParams;
          readonly isResult: BacktestResult;
        }
      | undefined;

    for (const params of candidates) {
      const isResult = yield* runOptimizeCandidate(
        engine,
        args,
        window.trainCandles,
        composerConfig,
        params,
      );
      if (isResult.totalTrades < args.minTrades) continue;
      if (
        selected === undefined ||
        objectiveValue(isResult, args.selectBy) >
          objectiveValue(selected.isResult, args.selectBy)
      ) {
        selected = { params, isResult };
      }
    }

    if (selected === undefined) return null;
    const oosResult = yield* runOptimizeCandidate(
      engine,
      args,
      window.testCandles,
      composerConfig,
      selected.params,
    );
    if (oosResult.totalTrades < args.minOosTrades) return null;
    return { ...selected, oosResult };
  });
}

function runOptimizeCandidate(
  engine: BacktestEngineImpl,
  args: OptimizeArgs,
  candles: readonly CandleLike[],
  composerConfig: ComposerConfig,
  params: OptimizeCandidateParams,
) {
  const slippageBps = args.realistic
    ? args.realisticSlippageBps
    : args.slippageBps;
  return engine.runBacktest({
    symbol: args.symbol,
    exchange: args.exchange,
    timeframe: args.timeframe,
    candles,
    composerConfig,
    initialCapital: args.capital,
    positionSizePct: args.positionSize,
    riskPerTradePct: args.riskPerTrade,
    maxPositionSizePct: args.maxPositionSize,
    stopLossPct: params.stopLossPct,
    takeProfitPct: params.takeProfitPct,
    feePct: args.fee,
    makerFeePct: args.makerFeePct,
    entryOrderType: params.entryOrderType,
    entryLimitOffsetBps: params.entryLimitOffsetBps,
    minConfidence: params.minConfidence,
    useAtrStops: params.useAtrStops,
    atrStopMultiplier: params.stopMult,
    atrTakeProfitMultiplier: params.tpMult,
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
    minEfficiencyRatio: params.minEfficiencyRatio,
    efficiencyRatioPeriod: args.efficiencyRatioPeriod,
    rsiLongMax: params.rsiLongMax,
    rsiShortMin: params.rsiShortMin,
    recordEquityCurve: false,
    htfCandles: [],
    breakevenAtR: params.breakevenAtR,
    maxBarsInTrade: params.maxBarsInTrade,
    lossCooldownBars: params.lossCooldownBars,
    autoRegimeFilter: args.autoRegimeFilter,
    autoRegimeAdxThreshold: args.autoRegimeAdxThreshold,
    entryOnClose: args.entryOnClose,
    useObservedPrice: args.observedPrice,
  });
}

function topOptimizeBy(
  results: ReadonlyArray<OptimizeResult>,
  metric: "totalReturnPct" | "sharpeRatio",
): OptimizeResult[] {
  return [...results]
    .sort(
      (a, b) =>
        (b.oosResult ?? b.isResult)[metric] -
        (a.oosResult ?? a.isResult)[metric],
    )
    .slice(0, 5);
}

export function formatOptimizeCandidateLine(r: OptimizeResult): string {
  const result = r.oosResult ?? r.isResult;
  return (
    `  stop=${r.params.stopMult.toFixed(2)} tp=${r.params.tpMult.toFixed(2)} conf=${r.params.minConfidence.toFixed(2)} | ` +
    `return=${result.totalReturnPct.toFixed(2)}% sharpe=${result.sharpeRatio.toFixed(3)} trades=${result.totalTrades} win=${(result.winRate * 100).toFixed(1)}% dd=${result.maxDrawdownPct.toFixed(2)}%`
  );
}

function logTopOptimizeCandidates(
  title: string,
  rows: ReadonlyArray<OptimizeResult>,
) {
  return Effect.gen(function* () {
    yield* Console.log(title);
    for (const r of rows) {
      yield* Console.log(formatOptimizeCandidateLine(r));
    }
  });
}

function logOosAggregate(
  oosResults: ReadonlyArray<BacktestResult>,
  initialCapital: number,
) {
  return Effect.gen(function* () {
    if (oosResults.length === 0 || initialCapital <= 0) return;
    let capital = initialCapital;
    let peak = capital;
    let maxDrawdownPct = 0;
    for (const result of oosResults) {
      const windowStartCapital = capital;
      const scale = windowStartCapital / initialCapital;
      for (const trade of result.trades) {
        capital += trade.netPnl * scale;
        peak = Math.max(peak, capital);
        maxDrawdownPct = Math.max(
          maxDrawdownPct,
          peak > 0 ? ((peak - capital) / peak) * 100 : 0,
        );
      }
    }
    const profitableWindows = oosResults.filter(
      (result) => result.totalReturnPct > 0,
    ).length;
    yield* Console.log(
      `Walk-forward OOS aggregate: windows=${oosResults.length} ` +
        `profitable=${((profitableWindows / oosResults.length) * 100).toFixed(1)}% ` +
        `compoundReturn=${(((capital - initialCapital) / initialCapital) * 100).toFixed(2)}% ` +
        `maxDD=${maxDrawdownPct.toFixed(2)}% ` +
        `trades=${oosResults.reduce((sum, result) => sum + result.totalTrades, 0)}`,
    );
  });
}

function printOptimizeResult(
  results: ReadonlyArray<OptimizeResult>,
  symbol: string,
  timeframe: string,
  initialCapital: number,
) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No optimization results.");
      return;
    }

    const byReturn = topOptimizeBy(results, "totalReturnPct");
    const bySharpe = topOptimizeBy(results, "sharpeRatio");

    yield* Console.log(
      `\n🔬 Optimization results for ${symbol} ${timeframe} (${results.length} configs tested)`,
    );
    const oosResults = results.flatMap((result) =>
      result.oosResult === undefined ? [] : [result.oosResult],
    );
    yield* logOosAggregate(oosResults, initialCapital);
    yield* logTopOptimizeCandidates("\nTop 5 by total return:", byReturn);
    yield* logTopOptimizeCandidates("\nTop 5 by Sharpe ratio:", bySharpe);
  });
}

export interface ScanArgs extends Omit<ResolvedBacktestArgs, "symbol"> {
  readonly symbol?: string;
  readonly minCandles: number;
  readonly top: number;
  readonly optimize: boolean;
  readonly minReturnPct: Option.Option<number>;
  readonly minSharpe: Option.Option<number>;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly saveWatchlist: Option.Option<string>;
  readonly watchlistPath?: string;
  readonly selectBy: "return" | "sharpe" | "calmar";
  readonly minTrades: number;
  readonly minOosTrades: number;
}
