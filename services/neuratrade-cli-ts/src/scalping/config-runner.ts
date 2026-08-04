import type { BacktestOptions } from "./backtest.js";
import type { PortfolioBacktestOptions } from "./portfolio-backtest.js";
import type { MultiSymbolPortfolioInput } from "./portfolio-backtest.js";
import {
  strategyConfigToComposerConfig,
  type StrategyConfig,
} from "./strategy-config.js";
import { computeSymbolStats } from "./symbol-stats.js";
import type { CandleLike, FundingRate } from "./types.js";

/**
 * Build a full `BacktestOptions` object from a unified `StrategyConfig`.
 *
 * Maps the execution and signal-rule fields that exist in the config schema,
 * supplies sensible defaults for fields not yet configurable, and derives the
 * composer config and per-symbol statistics automatically.
 */
export function buildBacktestOptionsFromConfig(
  config: StrategyConfig,
  symbol: string,
  exchange: string,
  timeframe: string,
  candles: readonly CandleLike[],
  initialCapital: number,
  fundingRates?: readonly FundingRate[],
): BacktestOptions {
  const execution = config.execution;
  const signalRules = config.signalRules;

  return {
    symbol,
    exchange,
    timeframe,
    candles,
    composerConfig: strategyConfigToComposerConfig(config),
    initialCapital,

    // Execution mapping
    positionSizePct: execution.positionSizePct,
    stopLossPct: execution.stopLossPct,
    takeProfitPct: execution.takeProfitPct,
    feePct: execution.feePct,
    makerFeePct: execution.makerFeePct,
    entryOrderType: execution.entryOrderType,
    entryLimitOffsetBps: execution.entryLimitOffsetBps,
    useAtrStops: execution.useAtrStops,
    atrStopMultiplier: execution.atrStopMultiplier,
    atrTakeProfitMultiplier: execution.atrTakeProfitMultiplier,
    atrRiskReward: execution.atrRiskReward,
    useAdaptiveStops: execution.useAdaptiveStops,
    adaptiveStopAtrMultiplier: execution.adaptiveStopAtrMultiplier,
    adaptiveRiskReward: execution.adaptiveRiskReward,
    scaleOutAtR: execution.scaleOutAtR,
    scaleOutPct: execution.scaleOutPct,
    riskPerTradePct: execution.riskPerTradePct,
    maxPositionSizePct: execution.maxPositionSizePct,
    volatilityTargetAnnualPct: execution.volatilityTargetAnnualPct,
    leverage: execution.leverage,
    slippageBps: execution.slippageBps,
    trailingStopPct: execution.trailingStopPct,
    trailingStopAtrMultiplier: execution.trailingStopAtrMultiplier,
    minAtrPct: execution.minAtrPct,
    volatilityLookback: execution.volatilityLookback,
    volatilityLowPct: execution.volatilityLowPct,
    volatilityHighPct: execution.volatilityHighPct,
    volatilityLowFactor: execution.volatilityLowFactor,
    volatilityHighFactor: execution.volatilityHighFactor,
    maxBarsInTrade: execution.maxBarsInTrade,
    lossCooldownBars: execution.lossCooldownBars,
    lossConfidencePenalty: execution.lossConfidencePenalty,
    lossConfidenceDecay: execution.lossConfidenceDecay,
    breakevenAtR: execution.breakevenAtR,
    holdUntilStop: execution.holdUntilStop,
    entryOnClose: execution.entryOnClose,
    useObservedPrice: execution.observedPrice ?? false,
    exitRsiPeriod: execution.exitRsiPeriod,
    exitRsiLongLevel: execution.exitRsiLongLevel,
    exitRsiShortLevel: execution.exitRsiShortLevel,

    // Signal-rule mapping
    minConfidence: signalRules.minConfidence,
    signalPersistence: signalRules.signalPersistence,
    sessionStart: signalRules.sessionStart,
    sessionEnd: signalRules.sessionEnd,
    autoRegimeFilter: signalRules.autoRegimeFilter,
    autoRegimeAdxThreshold: signalRules.autoRegimeAdxThreshold,

    // Sensible defaults for fields not yet in the config schema
    fundingRatePct: 0,
    fundingIntervalHours: 8,
    isFutures: false,
    htfSignalConfidence: 0,
    htfTrendFastPeriod: 50,
    htfTrendSlowPeriod: 100,
    entryPullbackEmaPeriod: 0,
    entryPullbackMarginPct: 0.1,
    minEfficiencyRatio: 0,
    efficiencyRatioPeriod: 20,
    rsiLongMax: 0,
    rsiShortMin: 0,
    bollingerLongMaxPctB: -1,
    bollingerShortMinPctB: 2,
    recordEquityCurve: false,
    oosPct: 0,
    mcIterations: 0,

    symbolStats: computeSymbolStats(candles, timeframe),
    fundingRates,
  };
}

/**
 * Build the options object expected by `runMultiSymbolPortfolioBacktest` from a
 * unified `StrategyConfig`.
 *
 * `symbols` and `candles` must be aligned arrays (one candle series per
 * symbol). The per-symbol backtest options are spread from the first symbol,
 * and `maxOpenPositions` is taken from the config (or an optional override).
 */
export function buildPortfolioOptionsFromConfig(
  config: StrategyConfig,
  exchange: string,
  timeframe: string,
  symbols: readonly string[],
  candles: readonly (readonly CandleLike[])[],
  initialCapital: number,
  maxOpenPositions?: number,
  fundingRates?: readonly (readonly FundingRate[] | undefined)[],
): Omit<PortfolioBacktestOptions, "symbol" | "candles" | "initialCapital"> & {
  readonly initialCapital: number;
  readonly symbols: readonly MultiSymbolPortfolioInput[];
} {
  if (symbols.length === 0) {
    throw new Error("At least one symbol is required for portfolio backtest");
  }
  if (candles.length !== symbols.length) {
    throw new Error(
      `symbols (${symbols.length}) and candles (${candles.length}) must have the same length`,
    );
  }

  const baseOptions = buildBacktestOptionsFromConfig(
    config,
    symbols[0] ?? "",
    exchange,
    timeframe,
    candles[0] ?? [],
    initialCapital,
  );

  const inputs: MultiSymbolPortfolioInput[] = symbols.map((symbol, i) => ({
    symbol,
    candles: candles[i] ?? [],
    fundingRates: fundingRates?.[i],
  }));

  return {
    exchange,
    timeframe,
    composerConfig: baseOptions.composerConfig,
    initialCapital,
    positionSizePct: baseOptions.positionSizePct,
    maxOpenPositions: maxOpenPositions ?? config.portfolio.maxOpenPositions,
    stopLossPct: baseOptions.stopLossPct,
    takeProfitPct: baseOptions.takeProfitPct,
    feePct: baseOptions.feePct,
    makerFeePct: baseOptions.makerFeePct,
    entryOrderType: baseOptions.entryOrderType,
    entryLimitOffsetBps: baseOptions.entryLimitOffsetBps,
    minConfidence: baseOptions.minConfidence,
    maxBarsInTrade: baseOptions.maxBarsInTrade,
    sessionStart: baseOptions.sessionStart,
    sessionEnd: baseOptions.sessionEnd,
    slippageBps: baseOptions.slippageBps,
    useAtrStops: config.execution.useAtrStops,
    atrStopMultiplier: config.execution.atrStopMultiplier,
    atrTakeProfitMultiplier: config.execution.atrTakeProfitMultiplier,
    atrRiskReward: config.execution.atrRiskReward,
    useAdaptiveStops: config.execution.useAdaptiveStops,
    adaptiveStopAtrMultiplier: config.execution.adaptiveStopAtrMultiplier,
    adaptiveRiskReward: config.execution.adaptiveRiskReward,
    scaleOutAtR: config.execution.scaleOutAtR,
    scaleOutPct: config.execution.scaleOutPct,
    volatilityLookback: config.execution.volatilityLookback,
    volatilityLowPct: config.execution.volatilityLowPct,
    volatilityHighPct: config.execution.volatilityHighPct,
    volatilityLowFactor: config.execution.volatilityLowFactor,
    volatilityHighFactor: config.execution.volatilityHighFactor,
    riskPerTradePct: config.execution.riskPerTradePct,
    maxPositionSizePct: config.execution.maxPositionSizePct,
    maxPortfolioHeatPct: config.portfolio.maxPortfolioHeatPct,
    correlationFilter: config.portfolio.correlationFilter,
    correlationLookback: config.portfolio.correlationLookback,
    correlationThreshold: config.portfolio.correlationThreshold,
    observedPrice: config.execution.observedPrice ?? false,
    symbols: inputs,
  };
}
