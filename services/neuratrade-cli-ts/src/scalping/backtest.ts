import type {
  CandleLike,
  ComposerConfig,
  ComposerThresholds,
  ComposerWeights,
  Direction,
  FundingRate,
  OHLCVInput,
  OrderBookMetricsInput,
  ScalpingSignal,
} from "./types.js";
import { composeSignal, defaultComposerConfig } from "./composer.js";
import {
  calculateADX,
  calculateAnnualizedVolatility,
  calculateATR,
  calculateBollingerBands,
  calculateEMA,
  calculateRSI,
} from "./indicators.js";
import { checkRsiExit, computeExitLevels } from "./exit-engine.js";
import { makeCausalSymbolStats } from "./symbol-stats.js";
import type { SymbolStatistics } from "./symbol-stats.js";
import {
  computePerformanceMetrics,
  robustnessScore,
  type BacktestMetrics,
} from "./performance-metrics.js";

/**
 * Build the warning message for a fee that looks like a fraction rather than
 * a percent, or return null when the value already looks like a percent.
 * Emitted via `Effect.logWarning` at the Effect boundary (see
 * `BacktestEngine` in `./services.js`) so this module stays side-effect free.
 */
export function feeFractionWarning(
  feePct: number,
  label: string,
): string | null {
  if (feePct > 0 && feePct < 0.01) {
    const corrected = feePct * 100;
    return `${label} looks like a fraction (${feePct}); engine expects percent (${corrected}). Multiplying by 100.`;
  }
  return null;
}

/**
 * Normalize a fee input to the percent convention expected by the engine.
 * The engine computes fees as notional * (feePct / 100), so feePct is a
 * percent per side (0.1 = 0.1%). Passing a fraction like 0.001 is a common
 * mistake; this helper detects values in the (0, 0.01) range and converts
 * them to percent.
 */
export function normalizeFeePct(feePct: number): number {
  if (feePct > 0 && feePct < 0.01) {
    return feePct * 100;
  }
  return feePct;
}

export function normalizeOptionalFeePct(
  feePct: number | undefined,
  _label: string,
): number | undefined {
  if (feePct === undefined) return undefined;
  return normalizeFeePct(feePct);
}

export interface EntryFillResult {
  readonly entryPrice: number;
  readonly appliedFeePct: number;
  readonly fillType: "maker" | "taker";
  readonly filled: boolean;
}

export function resolveEntryFill(
  rawEntry: number,
  next: CandleLike,
  side: "long" | "short",
  feePct: number,
  makerFeePct: number | undefined,
  entryOrderType: "market" | "limit",
  entryLimitOffsetBps: number,
  slippageBps: number,
): EntryFillResult {
  if (entryOrderType === "market") {
    return {
      entryPrice: applySlippage(
        rawEntry,
        side,
        slippageBps,
        next.high,
        next.low,
      ),
      appliedFeePct: feePct,
      fillType: "taker",
      filled: true,
    };
  }

  const offset = (rawEntry * (entryLimitOffsetBps ?? 0)) / 10000;
  const limitPrice =
    side === "long" ? Math.max(0, rawEntry - offset) : rawEntry + offset;

  const canFill =
    side === "long"
      ? next.low <= limitPrice + 1e-12
      : next.high >= limitPrice - 1e-12;

  if (!canFill) {
    return {
      entryPrice: limitPrice,
      appliedFeePct: feePct,
      fillType: "taker",
      filled: false,
    };
  }

  const appliedFeePct = makerFeePct !== undefined ? makerFeePct : feePct;
  return {
    entryPrice: limitPrice,
    appliedFeePct,
    fillType: makerFeePct !== undefined ? "maker" : "taker",
    filled: true,
  };
}

export interface BacktestPosition {
  readonly entrySignal: ScalpingSignal;
  readonly entryPrice: number;
  readonly entryTime: Date;
  readonly entryBarIndex: number;
  readonly side: "long" | "short";
  size: number;
  stopLoss: number;
  readonly takeProfit: number;
  readonly trailingStopAtr?: number;
  highestPrice: number;
  lowestPrice: number;
  scaledOut: boolean;
  readonly scaleOutPrice: number;
  readonly initialRiskPct: number;
  /** Total entry fee paid for this position (maker or taker). */
  entryFeePaid: number;
  /** Entry fee rate in percent charged for this position. */
  entryFeePct: number;
  /** Whether the entry fill was a maker or taker fill. */
  fillType: "maker" | "taker";
}

export interface BacktestTrade {
  readonly id: string;
  readonly symbol: string;
  readonly side: "long" | "short";
  readonly entryTime: Date;
  readonly exitTime: Date;
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly pnl: number;
  readonly pnlPct: number;
  /** Net cash effect of the trade including entry/exit fees and funding. */
  readonly netPnl: number;
  readonly exitReason:
    | "signal"
    | "stop_loss"
    | "take_profit"
    | "scale_out"
    | "liquidation"
    | "time_stop"
    | "rsi_exit";
  readonly initialRiskPct: number;
  /** Whether the entry fill was maker or taker. */
  readonly fillType: "maker" | "taker";
  /** Entry fee rate in percent charged for this trade. */
  readonly entryFeePct: number;
  /** Exit fee rate in percent charged for this trade. */
  readonly exitFeePct: number;
}

export interface BacktestResult {
  readonly symbol: string;
  readonly totalTrades: number;
  readonly winningTrades: number;
  readonly losingTrades: number;
  readonly winRate: number;
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly sharpeRatio: number;
  readonly trades: readonly BacktestTrade[];
  readonly totalFeesPaid: number;
  readonly totalFundingCost: number;
  readonly benchmarkReturnPct: number;
  readonly metrics: BacktestMetrics;
  /** Equity curve sampled at each trade close, captured only when requested. */
  readonly equityCurve?: readonly BacktestEquityPoint[];
  /** Out-of-sample result when an OOS split is requested. */
  readonly oosResult?: BacktestResult;
  /** Monte Carlo drawdown simulation based on the in-sample trades. */
  readonly monteCarlo?: MonteCarloResult;
  /** Maker fill rate: fraction of entry signals that filled as maker (0-1). */
  readonly makerFillRate?: number;
  /** Composite robustness score roughly in [-100, 100]; higher is better. */
  readonly robustnessScore: number;
}

export interface MonteCarloResult {
  readonly iterations: number;
  readonly medianMaxDrawdownPct: number;
  readonly p95MaxDrawdownPct: number;
  readonly p99MaxDrawdownPct: number;
  readonly worstMaxDrawdownPct: number;
  readonly probabilityOfRuinPct: number;
}

export interface BacktestEquityPoint {
  readonly tradeIndex: number;
  readonly timestamp: Date;
  readonly capital: number;
}

export interface BacktestOptions {
  readonly symbol: string;
  readonly exchange: string;
  readonly timeframe: string;
  readonly candles: readonly CandleLike[];
  readonly composerConfig: ComposerConfig;
  readonly initialCapital: number;
  readonly positionSizePct: number;
  readonly stopLossPct: number;
  readonly takeProfitPct: number;
  readonly feePct: number;
  /** Maker fee in percent per side (may be negative for rebates). When omitted, every fill is charged the taker rate. */
  readonly makerFeePct?: number;
  /** Entry order type. "market" uses the taker rate; "limit" may earn the maker rate if the bar trades through the limit. */
  readonly entryOrderType?: "market" | "limit";
  /** How far inside the bar the limit is placed relative to the raw entry price, in basis points. Default 0. */
  readonly entryLimitOffsetBps?: number;
  readonly minConfidence: number;
  /** When true, use ATR(14) * multiplier for dynamic stops instead of fixed pct. */
  readonly useAtrStops?: boolean;
  readonly atrStopMultiplier?: number;
  readonly atrTakeProfitMultiplier?: number;
  /** When true, ignore opposite-direction signal exits and only exit on stop/take-profit. */
  readonly holdUntilStop?: boolean;
  /** Slippage in basis points applied to entry and exit fills. Default 0. */
  readonly slippageBps?: number;
  /** Per-interval funding rate as whole-number percent (e.g. 0.01 = 0.01%). Default 0. */
  readonly fundingRatePct?: number;
  /** Hours between funding settlements. Default 8. */
  readonly fundingIntervalHours?: number;
  /** When true, accumulate funding costs on positions. Default false. */
  readonly isFutures?: boolean;
  /** Trail the stop-loss this percentage behind the most favorable price once in profit. */
  readonly trailingStopPct?: number;
  /** Trail the stop-loss at this ATR multiplier behind the most favorable price. */
  readonly trailingStopAtrMultiplier?: number;
  /** Minimum ATR% (ATR / price) required to enter a trade. Filters dead markets. */
  readonly minAtrPct?: number;
  /** When true, stop/target distances are derived from per-symbol ATR% stats. */
  readonly useAdaptiveStops?: boolean;
  /** Stop distance multiplier applied to the symbol's median ATR%. Default 1. */
  readonly adaptiveStopAtrMultiplier?: number;
  /** Risk:reward ratio applied to the adaptive stop distance. Default 2. */
  readonly adaptiveRiskReward?: number;
  /** When using ATR stops, set take-profit distance = stop distance * atrRiskReward.
   *  Overrides the legacy atrTakeProfitMultiplier behavior when > 0. */
  readonly atrRiskReward?: number;
  /** Partial scale-out trigger in R multiples (e.g. 1.0 = +1R). 0 disables. */
  readonly scaleOutAtR?: number;
  /** Percentage of the position to close at the scale-out trigger. Default 50. */
  readonly scaleOutPct?: number;
  /** Lookback window length for ATR% volatility percentile calibration. 0 disables. */
  readonly volatilityLookback?: number;
  /** Low percentile threshold for volatility calibration. */
  readonly volatilityLowPct?: number;
  /** High percentile threshold for volatility calibration. */
  readonly volatilityHighPct?: number;
  /** Multiplier applied to atrStopMultiplier in low-volatility regimes. */
  readonly volatilityLowFactor?: number;
  /** Multiplier applied to atrStopMultiplier in high-volatility regimes. */
  readonly volatilityHighFactor?: number;
  /** Annualized volatility target for position sizing. 0 disables vol-target sizing. */
  readonly volatilityTargetAnnualPct?: number;
  /** Risk a fixed percentage of current capital per trade instead of using a fixed position size.
   *  Position size = (capital * riskPerTradePct) / stopDistancePct. Overrides positionSizePct. */
  readonly riskPerTradePct?: number;
  /** Maximum position size as a percentage of capital when riskPerTradePct is used. */
  readonly maxPositionSizePct?: number;
  /** Require the same directional signal for this many consecutive candles before entering.
   *  0/1 disables the filter. Helps filter whipsaws. */
  readonly signalPersistence?: number;
  /** Additional min-confidence penalty applied after a losing trade.
   *  Decays by decay per candle. Helps avoid revenge trades in choppy regimes. */
  readonly lossConfidencePenalty?: number;
  readonly lossConfidenceDecay?: number;
  /** Optional higher-timeframe candles for a trend filter. When provided, entries
   *  are only allowed when the trade direction aligns with the HTF trend. */
  readonly htfCandles?: readonly CandleLike[];
  /** HTF EMA fast period. Default 50. */
  readonly htfTrendFastPeriod?: number;
  /** HTF EMA slow period. Default 100. */
  readonly htfTrendSlowPeriod?: number;
  /** Require the HTF composer signal to agree with the trade direction and
   *  meet this confidence threshold. 0 disables the HTF signal confluence filter. */
  readonly htfSignalConfidence?: number;
  /** When > 0, only enter when price is within this percentage of the EMA of
   *  the given period (pullback / value-entry filter). 0 disables. */
  readonly entryPullbackEmaPeriod?: number;
  readonly entryPullbackMarginPct?: number;
  /** Kaufman Efficiency Ratio filter. When > 0, only enter when the ER over the
   *  configured lookback is >= this value (0.0-1.0). 0 disables.
   *  High ER = strong trend, low ER = chop. */
  readonly minEfficiencyRatio?: number;
  readonly efficiencyRatioPeriod?: number;
  /** Leading RSI neutral-zone filter. For longs, require RSI <= rsiLongMax.
   *  For shorts, require RSI >= rsiShortMin. 0 disables. */
  readonly rsiLongMax?: number;
  readonly rsiShortMin?: number;
  /** Leading Bollinger %B pullback filter. For longs, require %B <= bollingerLongMaxPctB.
   *  For shorts, require %B >= bollingerShortMinPctB. Values outside [0,1] disable. */
  readonly bollingerLongMaxPctB?: number;
  readonly bollingerShortMinPctB?: number;
  /** When true, record the equity curve at each trade close. */
  readonly recordEquityCurve?: boolean;
  /** Percentage of candles (0-100) to hold out for out-of-sample validation.
   *  0 disables OOS. Default 0. */
  readonly oosPct?: number;
  /** Number of Monte Carlo permutations to run for drawdown simulation.
   *  0 disables MC. Default 0. */
  readonly mcIterations?: number;
  /** Futures leverage multiplier. 1 = spot-style (no liquidation risk).
   *  Applied to position size and liquidation checks when isFutures is true. */
  readonly leverage?: number;
  /** Move stop-loss to breakeven once price has moved this many R in profit.
   *  0 disables. */
  readonly breakevenAtR?: number;
  /** Maximum number of candles to hold a position. 0 disables time-stop. */
  readonly maxBarsInTrade?: number;
  /** Bars to skip after a losing trade before allowing a new entry.
   *  0 disables loss cooldown. */
  readonly lossCooldownBars?: number;
  /** UTC trading session start in "HH:MM" format. Empty string disables. */
  readonly sessionStart?: string;
  /** UTC trading session end in "HH:MM" format. Empty string disables. */
  readonly sessionEnd?: string;
  /** When true, block entries that do not match the detected market regime. */
  readonly autoRegimeFilter?: boolean;
  /** ADX threshold used by the auto-regime filter. Default 25. */
  readonly autoRegimeAdxThreshold?: number;

  /** When true, enter at the current candle's close instead of the next open.
   *  Reduces signal delay but assumes you can act before the bar closes. */
  readonly entryOnClose?: boolean;
  /** When true, use only observed close prices for stop/target/scale/trail
   *  exits instead of optimistic candle high/low assumptions. */
  readonly useObservedPrice?: boolean;
  /** Pre-computed per-symbol market statistics. When omitted, the engine will
   *  compute them internally if needed. */
  readonly symbolStats?: SymbolStatistics;

  /** Historical candles preceding `candles`, used only as context: causal
   *  symbol statistics and indicator windows see them, but they never
   *  generate trades. Set by the OOS split path so a split run sees the same
   *  per-bar context as a continuous full-period run (bd clever-cabin-dt8). */
  readonly warmupCandles?: readonly CandleLike[];

  /** Historical funding rates for the funding bias signal component. */
  readonly fundingRates?: readonly FundingRate[];

  /** RSI-based dynamic exit. Long exits when RSI(exitRsiPeriod) >= exitRsiLongLevel;
   *  short exits when RSI(exitRsiPeriod) <= exitRsiShortLevel. 0 disables. */
  readonly exitRsiPeriod?: number;
  readonly exitRsiLongLevel?: number;
  readonly exitRsiShortLevel?: number;
}

/**
 * Walk through historical candles and simulate scalping trades based on
 * deterministic signal composition.
 *
 * When `oosPct` is set, the candle series is split into an in-sample period
 * (used for the primary result) and an out-of-sample period (returned in
 * `oosResult`). When `mcIterations` is set, a Monte Carlo drawdown simulation
 * is run on the in-sample trade sequence.
 */
export function runBacktest(options: BacktestOptions): BacktestResult {
  const oosPct = Math.max(0, Math.min(100, options.oosPct ?? 0));
  const mcIterations = Math.max(0, Math.floor(options.mcIterations ?? 0));

  if (oosPct > 0) {
    const { is: isCandles, oos: oosCandles } = splitCandlesByOos(
      options.candles,
      oosPct,
    );
    if (isCandles.length < 20 || oosCandles.length < 20) {
      const base = runBacktestCore({ ...options, candles: options.candles });
      return attachMonteCarlo(base, options.initialCapital, mcIterations);
    }
    const isResult = runBacktestCore({
      ...options,
      candles: isCandles,
      recordEquityCurve: options.recordEquityCurve,
    });
    const oosResult = runBacktestCore({
      ...options,
      candles: oosCandles,
      warmupCandles: isCandles,
      recordEquityCurve: options.recordEquityCurve,
    });
    return {
      ...attachMonteCarlo(isResult, options.initialCapital, mcIterations),
      oosResult,
    };
  }

  const base = runBacktestCore(options);
  return attachMonteCarlo(base, options.initialCapital, mcIterations);
}

/** A single composer configuration candidate for a parameter sweep. */
export interface ComposerSweepCandidate {
  readonly name: string;
  /** Deep-merged over the base composer config (candidate fields win). */
  readonly composerConfig: {
    readonly weights?: Partial<ComposerWeights>;
    readonly thresholds?: Partial<ComposerThresholds>;
    readonly enabled?: ComposerConfig["enabled"];
  };
}

/** Ranked result of one candidate in a composer parameter sweep. */
export interface ComposerSweepResult {
  readonly name: string;
  readonly result: BacktestResult;
  readonly totalReturnPct: number;
  readonly sharpeRatio: number;
  readonly maxDrawdownPct: number;
  readonly totalTrades: number;
  readonly winRate: number;
  readonly robustnessScore: number;
}

/**
 * Sweep composer configurations over a fixed set of backtest options to find
 * profitable setups. Each candidate is merged over the base composer config
 * (candidate fields win) and run through `runBacktest`. Results are sorted by
 * total return descending so the most profitable configs surface first.
 */
export function sweepComposerConfigs(
  baseOptions: Omit<BacktestOptions, "composerConfig">,
  candidates: readonly ComposerSweepCandidate[],
  baseComposerConfig: ComposerConfig = defaultComposerConfig,
): ComposerSweepResult[] {
  const results: ComposerSweepResult[] = [];
  for (const candidate of candidates) {
    const merged: ComposerConfig = {
      ...baseComposerConfig,
      weights: {
        ...baseComposerConfig.weights,
        ...candidate.composerConfig.weights,
      },
      thresholds: {
        ...baseComposerConfig.thresholds,
        ...candidate.composerConfig.thresholds,
      },
      enabled: candidate.composerConfig.enabled ?? baseComposerConfig.enabled,
    };
    const result = runBacktest({ ...baseOptions, composerConfig: merged });
    results.push({
      name: candidate.name,
      result,
      totalReturnPct: result.totalReturnPct,
      sharpeRatio: result.sharpeRatio,
      maxDrawdownPct: result.maxDrawdownPct,
      totalTrades: result.totalTrades,
      winRate: result.winRate,
      robustnessScore: result.robustnessScore,
    });
  }
  return results.sort((a, b) => b.totalReturnPct - a.totalReturnPct);
}

/**
 * Convenience builder for a sweep candidate from a partial threshold override.
 * Spreads are merged over the base thresholds so callers only specify the
 * fields they want to vary.
 */
export function composerSweepCandidate(
  name: string,
  overrides: Partial<ComposerConfig["thresholds"]> & {
    weights?: Partial<ComposerConfig["weights"]>;
  },
): ComposerSweepCandidate {
  return {
    name,
    composerConfig: {
      weights: overrides.weights,
      thresholds: overrides,
    },
  };
}

/**
 * Assess whether a backtest result looks too good to be true.
 */
export interface RealismAssessment {
  readonly ok: boolean;
  readonly errors: readonly string[];
  readonly warnings: readonly string[];
}

export function assessBacktestRealism(
  result: BacktestResult,
  options: {
    readonly entryOrderType?: "market" | "limit";
    readonly strict?: boolean;
  } = {},
): RealismAssessment {
  const errors: string[] = [];
  const warnings: string[] = [];

  if (result.totalTrades >= 5 && result.winRate === 1) {
    errors.push("100% win rate with 5+ trades is unrealistic.");
  }

  if (
    result.totalTrades >= 5 &&
    result.losingTrades === 0 &&
    result.totalReturnPct > 0
  ) {
    errors.push(
      "Zero losing trades and positive return with 5+ trades is unrealistic.",
    );
  }

  if (
    options.entryOrderType === "limit" &&
    result.makerFillRate === 1 &&
    result.totalTrades >= 5
  ) {
    errors.push(
      "100% maker fill rate on limit entries with 5+ trades is unrealistic.",
    );
  }

  if (result.totalTrades < 10) {
    warnings.push(
      `Small sample size (${result.totalTrades} trades); results may not be reliable.`,
    );
  }

  if (
    options.entryOrderType === "limit" &&
    result.makerFillRate !== undefined &&
    result.makerFillRate > 0.95
  ) {
    warnings.push(
      `Maker fill rate ${(result.makerFillRate * 100).toFixed(1)}% is very high; verify limit placement assumptions.`,
    );
  }

  if (
    result.totalReturnPct > 0 &&
    result.winRate > 0.85 &&
    result.totalTrades >= 10
  ) {
    warnings.push(
      "Suspiciously high win rate; verify costs and fill assumptions.",
    );
  }

  return {
    ok: errors.length === 0,
    errors,
    warnings,
  };
}

export function attachMonteCarlo(
  result: BacktestResult,
  initialCapital: number,
  iterations: number,
): BacktestResult {
  if (iterations <= 0 || result.trades.length === 0) {
    return result;
  }
  return {
    ...result,
    monteCarlo: runMonteCarlo(result.trades, initialCapital, iterations),
  };
}

interface BacktestCoreRuntime {
  readonly options: BacktestOptions;
  readonly feePct: number;
  readonly makerFeePct: number | undefined;
  readonly entryOrderType: "market" | "limit";
  readonly entryLimitOffsetBps: number;
  readonly slippageBps: number;
  readonly fundingRatePct: number;
  readonly fundingIntervalMs: number;
  readonly isFutures: boolean;
  readonly leverage: number;
  readonly breakevenAtR: number;
  readonly maxBarsInTrade: number;
  readonly lossCooldownBars: number;
  readonly sessionStart: string;
  readonly sessionEnd: string;
  readonly autoRegimeFilter: boolean;
  readonly autoRegimeAdxThreshold: number;
  readonly useObservedPrice: boolean;
  readonly signalPersistence: number;
  readonly lossConfidencePenalty: number;
  readonly lossConfidenceDecay: number;
  readonly trades: BacktestTrade[];
  equityCurve: BacktestEquityPoint[] | undefined;
  position: BacktestPosition | null;
  capital: number;
  peakCapital: number;
  maxDrawdown: number;
  tradeId: number;
  totalFeesPaid: number;
  totalFundingCost: number;
  lastFundingTime: Date | null;
  entryAttempts: number;
  makerFills: number;
  priorSignalDirection: Direction;
  signalStreak: number;
  lossPenalty: number;
  cooldownBarsRemaining: number;
}

interface BacktestBarContext {
  readonly index: number;
  readonly current: CandleLike;
  readonly next: CandleLike;
  readonly window: readonly CandleLike[];
  readonly signal: ScalpingSignal | null;
  readonly symbolStats: SymbolStatistics;
}

interface BacktestEntryPlan {
  readonly entryPrice: number;
  readonly entryFeePct: number;
  readonly fillType: "maker" | "taker";
  readonly stopLoss: number;
  readonly takeProfit: number;
  readonly scaleOutPrice: number;
  readonly trailingStopAtr: number | undefined;
  readonly initialRiskPct: number;
  readonly size: number;
  readonly entryFee: number;
}

function recordBacktestEquityPoint(
  runtime: BacktestCoreRuntime,
  timestamp: Date,
): void {
  if (!runtime.equityCurve) return;
  runtime.equityCurve.push({
    tradeIndex: runtime.trades.length - 1,
    timestamp,
    capital: runtime.capital,
  });
}

function closeBacktestPosition(
  runtime: BacktestCoreRuntime,
  exitPrice: number,
  exitTime: Date,
  fundingTime: Date,
  exitReason: BacktestTrade["exitReason"],
  pnlOverride?: number,
  liquidation = false,
): void {
  const current = runtime.position;
  if (!current) return;
  const notional = current.entryPrice * current.size;
  const pnl = pnlOverride ?? calculatePnl(current, exitPrice);
  const exitFee = exitPrice * current.size * (runtime.feePct / 100);
  const funding = chargeFunding(
    current,
    runtime.lastFundingTime!,
    fundingTime,
    runtime.fundingRatePct,
    runtime.fundingIntervalMs,
    runtime.isFutures,
    runtime.options.fundingRates,
  );
  const actualPnl = liquidation ? -notional / runtime.leverage : pnl;
  const pnlPct = ((actualPnl - exitFee) / notional) * 100;
  runtime.capital += actualPnl - exitFee - funding.funding;
  runtime.totalFeesPaid += exitFee;
  runtime.totalFundingCost += funding.funding;
  runtime.lastFundingTime = funding.newLastFundingTime;
  runtime.trades.push({
    id: `trade-${runtime.tradeId++}`,
    symbol: runtime.options.symbol,
    side: current.side,
    entryTime: current.entryTime,
    exitTime,
    entryPrice: current.entryPrice,
    exitPrice,
    pnl: actualPnl,
    pnlPct,
    netPnl: actualPnl - exitFee - current.entryFeePaid - funding.funding,
    exitReason,
    initialRiskPct: current.initialRiskPct,
    fillType: current.fillType,
    entryFeePct: current.entryFeePct,
    exitFeePct: runtime.feePct,
  });
  recordBacktestEquityPoint(runtime, exitTime);
  if (actualPnl - exitFee - funding.funding < 0) {
    runtime.lossPenalty = runtime.lossConfidencePenalty;
    runtime.cooldownBarsRemaining = runtime.lossCooldownBars;
  }
  runtime.position = null;
  runtime.lastFundingTime = null;
}

function backtestExitSide(position: BacktestPosition): "long" | "short" {
  return position.side === "long" ? "short" : "long";
}

function backtestExitPrice(
  runtime: BacktestCoreRuntime,
  price: number,
  side: "long" | "short",
  candle: CandleLike,
): number {
  if (runtime.useObservedPrice) {
    return applyObservedSlippage(price, side, runtime.slippageBps);
  }
  return applySlippage(
    price,
    side,
    runtime.slippageBps,
    candle.high,
    candle.low,
  );
}

function updateBacktestMarketState(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): void {
  const position = runtime.position;
  if (!position) return;
  const mtmPnl = calculatePnl(position, bar.current.close);
  const mtmCapital = runtime.capital + mtmPnl;
  if (mtmCapital > runtime.peakCapital) runtime.peakCapital = mtmCapital;
  const drawdown = (runtime.peakCapital - mtmCapital) / runtime.peakCapital;
  if (drawdown > runtime.maxDrawdown) runtime.maxDrawdown = drawdown;
  updateTrailingStop(
    position,
    bar.current,
    runtime.options,
    runtime.useObservedPrice,
  );
  if (runtime.breakevenAtR > 0) {
    applyBreakevenStop(position, bar.current.close, runtime.breakevenAtR);
  }
}

function handleBacktestLiquidation(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): boolean {
  const position = runtime.position;
  if (
    !position ||
    runtime.leverage <= 1 ||
    !isLiquidated(position, bar.current.close, runtime.leverage)
  ) {
    return false;
  }
  const exitSide = backtestExitSide(position);
  const exitPrice = backtestExitPrice(
    runtime,
    bar.current.close,
    exitSide,
    bar.current,
  );
  const notional = position.entryPrice * position.size;
  closeBacktestPosition(
    runtime,
    exitPrice,
    bar.current.timestamp,
    bar.current.timestamp,
    "liquidation",
    -notional / runtime.leverage,
    true,
  );
  return true;
}

function handleBacktestTimeStop(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): boolean {
  const position = runtime.position;
  if (
    !position ||
    runtime.maxBarsInTrade <= 0 ||
    bar.index - position.entryBarIndex + 1 < runtime.maxBarsInTrade
  ) {
    return false;
  }
  const exitSide = backtestExitSide(position);
  const exitPrice = backtestExitPrice(
    runtime,
    bar.current.close,
    exitSide,
    bar.current,
  );
  closeBacktestPosition(
    runtime,
    exitPrice,
    bar.current.timestamp,
    bar.current.timestamp,
    "time_stop",
  );
  return true;
}

function recordBacktestScaleOut(
  runtime: BacktestCoreRuntime,
  position: BacktestPosition,
  partialSize: number,
  exitPrice: number,
  timestamp: Date,
): void {
  const pnl = calculatePnl({ ...position, size: partialSize }, exitPrice);
  const exitFee = exitPrice * partialSize * (runtime.feePct / 100);
  const entryFee =
    position.size > 0
      ? position.entryFeePaid * (partialSize / position.size)
      : 0;
  const pnlPct = ((pnl - exitFee) / (position.entryPrice * partialSize)) * 100;
  runtime.capital += pnl - exitFee;
  runtime.totalFeesPaid += exitFee;
  runtime.trades.push({
    id: `trade-${runtime.tradeId++}`,
    symbol: runtime.options.symbol,
    side: position.side,
    entryTime: position.entryTime,
    exitTime: timestamp,
    entryPrice: position.entryPrice,
    exitPrice,
    pnl,
    pnlPct,
    netPnl: pnl - exitFee - entryFee,
    exitReason: "scale_out",
    initialRiskPct: position.initialRiskPct,
    fillType: position.fillType,
    entryFeePct: position.entryFeePct,
    exitFeePct: runtime.feePct,
  });
  recordBacktestEquityPoint(runtime, timestamp);
  if (pnl - exitFee < 0)
    runtime.cooldownBarsRemaining = runtime.lossCooldownBars;
}

function backtestScaleOutPct(runtime: BacktestCoreRuntime): number {
  return Math.max(0, Math.min(100, runtime.options.scaleOutPct ?? 50));
}

function handleBacktestScaleOut(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): boolean {
  const position = runtime.position;
  if (
    !position ||
    !checkScaleOut(position, bar.current, runtime.useObservedPrice)
  ) {
    return false;
  }
  const partialSize = position.size * (backtestScaleOutPct(runtime) / 100);
  if (partialSize <= 0) return true;
  const exitSide = backtestExitSide(position);
  const exitPrice = backtestExitPrice(
    runtime,
    position.scaleOutPrice,
    exitSide,
    bar.current,
  );
  recordBacktestScaleOut(
    runtime,
    position,
    partialSize,
    exitPrice,
    bar.current.timestamp,
  );
  position.size -= partialSize;
  position.stopLoss = position.entryPrice;
  position.scaledOut = true;
  return true;
}

function handleBacktestPriceExit(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): boolean {
  const position = runtime.position;
  if (!position) return false;
  const exits = checkExitLevels(
    position,
    bar.current,
    runtime.useObservedPrice,
  );
  if (!exits.stopLoss && !exits.takeProfit) return false;
  const exitSide = backtestExitSide(position);
  const exitLevel = exits.stopLoss ? position.stopLoss : position.takeProfit;
  const exitPrice = backtestExitPrice(
    runtime,
    exitLevel,
    exitSide,
    bar.current,
  );
  closeBacktestPosition(
    runtime,
    exitPrice,
    bar.current.timestamp,
    bar.current.timestamp,
    exits.stopLoss ? "stop_loss" : "take_profit",
  );
  return true;
}

function handleBacktestRsiExit(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): boolean {
  const position = runtime.position;
  if (
    !position ||
    !checkRsiExit({
      side: position.side,
      candles: bar.window,
      exitRsiPeriod: runtime.options.exitRsiPeriod,
      exitRsiLongLevel: runtime.options.exitRsiLongLevel,
      exitRsiShortLevel: runtime.options.exitRsiShortLevel,
    })
  ) {
    return false;
  }
  const exitSide = backtestExitSide(position);
  const exitCandle = runtime.useObservedPrice ? bar.current : bar.next;
  const exitPrice = backtestExitPrice(
    runtime,
    runtime.useObservedPrice ? bar.current.close : bar.next.open,
    exitSide,
    exitCandle,
  );
  const exitTime = runtime.useObservedPrice
    ? bar.current.timestamp
    : bar.next.timestamp;
  closeBacktestPosition(runtime, exitPrice, exitTime, exitTime, "rsi_exit");
  return true;
}

function handleBacktestSignalExit(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): void {
  const position = runtime.position;
  if (
    !position ||
    runtime.options.holdUntilStop ||
    !bar.signal ||
    !shouldExitPosition(position, bar.signal)
  ) {
    return;
  }
  const exitSide = backtestExitSide(position);
  const exitPrice = backtestExitPrice(
    runtime,
    bar.next.open,
    exitSide,
    bar.next,
  );
  closeBacktestPosition(
    runtime,
    exitPrice,
    bar.next.timestamp,
    bar.next.timestamp,
    "signal",
  );
}

function manageBacktestPosition(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): boolean {
  if (!runtime.position) return false;
  updateBacktestMarketState(runtime, bar);
  if (handleBacktestLiquidation(runtime, bar)) return true;
  if (handleBacktestTimeStop(runtime, bar)) return true;
  if (handleBacktestScaleOut(runtime, bar)) return true;
  if (handleBacktestPriceExit(runtime, bar)) return true;
  if (handleBacktestRsiExit(runtime, bar)) return true;
  handleBacktestSignalExit(runtime, bar);
  return false;
}

function updateBacktestSignalState(
  runtime: BacktestCoreRuntime,
  signal: ScalpingSignal | null,
): void {
  const direction = signal?.direction ?? "hold";
  if (direction !== "hold" && direction === runtime.priorSignalDirection) {
    runtime.signalStreak += 1;
  } else {
    runtime.signalStreak = direction === "hold" ? 0 : 1;
  }
  runtime.priorSignalDirection = direction;
  if (runtime.lossPenalty > 0 && runtime.lossConfidenceDecay > 0) {
    runtime.lossPenalty = Math.max(
      0,
      runtime.lossPenalty - runtime.lossConfidenceDecay,
    );
  }
}

function passesBacktestBollingerFilter(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
  side: "long" | "short",
): boolean {
  const { bollingerLongMaxPctB, bollingerShortMinPctB } = runtime.options;
  const useLong =
    bollingerLongMaxPctB !== undefined &&
    bollingerLongMaxPctB >= 0 &&
    bollingerLongMaxPctB <= 1;
  const useShort =
    bollingerShortMinPctB !== undefined &&
    bollingerShortMinPctB >= 0 &&
    bollingerShortMinPctB <= 1;
  if (!useLong && !useShort) return true;
  return passesBollingerPullback(
    bar.window,
    side,
    useLong ? bollingerLongMaxPctB! : 1,
    useShort ? bollingerShortMinPctB! : 0,
  );
}

function passesBacktestEntryFilters(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
  side: "long" | "short",
): boolean {
  const options = runtime.options;
  if (options.htfCandles && options.htfCandles.length > 0) {
    const closedHtfCandles = options.htfCandles.filter(
      (htf) => htf.timestamp.getTime() <= bar.current.timestamp.getTime(),
    );
    if (
      !alignsWithHigherTimeframeTrend(
        side,
        closedHtfCandles,
        options.htfTrendFastPeriod,
        options.htfTrendSlowPeriod,
      )
    ) {
      return false;
    }
  }
  if (
    options.entryPullbackEmaPeriod &&
    options.entryPullbackEmaPeriod > 0 &&
    !priceWithinEmaPullback(
      bar.current.close,
      bar.window,
      options.entryPullbackEmaPeriod,
      options.entryPullbackMarginPct ?? 0.1,
    )
  ) {
    return false;
  }
  if (
    options.minEfficiencyRatio &&
    options.minEfficiencyRatio > 0 &&
    efficiencyRatio(bar.window, options.efficiencyRatioPeriod ?? 20) <
      options.minEfficiencyRatio
  ) {
    return false;
  }
  if (
    (options.rsiLongMax || options.rsiShortMin) &&
    !passesRsiNeutralZone(
      bar.window,
      side,
      options.rsiLongMax ?? 100,
      options.rsiShortMin ?? 0,
    )
  ) {
    return false;
  }
  return passesBacktestBollingerFilter(runtime, bar, side);
}

function isBacktestEntryEligible(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): boolean {
  if (runtime.position) return false;
  if (runtime.cooldownBarsRemaining !== 0) return false;
  if (
    !bar.signal ||
    !isEntrySignal(
      bar.signal,
      runtime.options.minConfidence + runtime.lossPenalty,
    )
  ) {
    return false;
  }
  const persistenceMet =
    runtime.signalPersistence <= 1 ||
    runtime.signalStreak >= runtime.signalPersistence;
  if (!persistenceMet) return false;
  if (
    !isWithinSession(
      bar.current.timestamp,
      runtime.sessionStart,
      runtime.sessionEnd,
    )
  ) {
    return false;
  }
  if (
    runtime.autoRegimeFilter &&
    !passesAutoRegimeFilter(
      bar.window,
      bar.signal.direction === "buy" ? "long" : "short",
      runtime.autoRegimeAdxThreshold,
    )
  ) {
    return false;
  }
  return true;
}

interface BacktestAtrPlan {
  readonly atr: number | null;
  readonly useAtr: boolean;
  readonly stopMultiplier: number;
  readonly riskReward: number;
}

function resolveBacktestAtrPlan(
  options: BacktestOptions,
  window: readonly CandleLike[],
  entryPrice: number,
): BacktestAtrPlan | null {
  const needsAtr =
    options.useAtrStops ||
    options.trailingStopAtrMultiplier ||
    options.minAtrPct;
  const atr = needsAtr ? calculateATR(window, 14) : null;
  const useAtr = options.useAtrStops && atr !== null && atr > 0;
  const stopMultiplier = options.atrStopMultiplier ?? 1.5;
  const takeProfitMultiplier = options.atrTakeProfitMultiplier ?? 2.5;
  if (options.minAtrPct && options.minAtrPct > 0) {
    const atrPct = atr && entryPrice > 0 ? atr / entryPrice : 0;
    if (atrPct < options.minAtrPct / 100) return null;
  }
  const riskReward =
    options.atrRiskReward && options.atrRiskReward > 0
      ? options.atrRiskReward
      : takeProfitMultiplier / stopMultiplier;
  return { atr, useAtr: !!useAtr, stopMultiplier, riskReward };
}

function computeBacktestEntryExits(
  options: BacktestOptions,
  bar: BacktestBarContext,
  side: "long" | "short",
  entryPrice: number,
  atrPlan: BacktestAtrPlan,
): ReturnType<typeof computeExitLevels> {
  return computeExitLevels({
    side,
    entryPrice,
    atr: atrPlan.atr,
    useAtr: atrPlan.useAtr,
    atrStopMultiplier: atrPlan.stopMultiplier,
    atrRiskReward: atrPlan.riskReward,
    stopLossPct: options.stopLossPct,
    takeProfitPct: options.takeProfitPct,
    scaleOutAtR: options.scaleOutAtR ?? 0,
    candles: bar.window,
    volatilityLookback: options.volatilityLookback ?? 0,
    volatilityLowPct: options.volatilityLowPct ?? 20,
    volatilityHighPct: options.volatilityHighPct ?? 80,
    volatilityLowFactor: options.volatilityLowFactor ?? 0.8,
    volatilityHighFactor: options.volatilityHighFactor ?? 1.2,
    symbolStats: bar.symbolStats,
    useAdaptiveStops: options.useAdaptiveStops,
    adaptiveStopAtrMultiplier: options.adaptiveStopAtrMultiplier,
    adaptiveRiskReward: options.adaptiveRiskReward,
  });
}

function resolveBacktestEntryPlan(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
  side: "long" | "short",
  fill: EntryFillResult,
): BacktestEntryPlan | null {
  const options = runtime.options;
  const atrPlan = resolveBacktestAtrPlan(options, bar.window, fill.entryPrice);
  if (!atrPlan) return null;
  const exits = computeBacktestEntryExits(
    options,
    bar,
    side,
    fill.entryPrice,
    atrPlan,
  );
  const stopDistancePct =
    fill.entryPrice > 0
      ? Math.abs(fill.entryPrice - exits.stopLoss) / fill.entryPrice
      : 0;
  const currentVolatility = calculateAnnualizedVolatility(
    bar.window,
    options.volatilityLookback ?? 0,
    options.timeframe,
  );
  const positionValue = calculatePositionValue(
    runtime.capital,
    fill.entryPrice,
    stopDistancePct,
    currentVolatility,
    options,
  );
  const size = positionValue / fill.entryPrice;
  return {
    entryPrice: fill.entryPrice,
    entryFeePct: fill.appliedFeePct,
    fillType: fill.fillType,
    stopLoss: exits.stopLoss,
    takeProfit: exits.takeProfit,
    scaleOutPrice: exits.scaleOutPrice ?? 0,
    trailingStopAtr: atrPlan.useAtr ? (atrPlan.atr ?? undefined) : undefined,
    initialRiskPct: stopDistancePct,
    size,
    entryFee: positionValue * (fill.appliedFeePct / 100),
  };
}

function tryOpenBacktestPosition(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): void {
  if (!isBacktestEntryEligible(runtime, bar) || !bar.signal) return;
  const side = bar.signal.direction === "buy" ? "long" : "short";
  if (!passesBacktestEntryFilters(runtime, bar, side)) return;
  const entryOnClose = runtime.options.entryOnClose ?? false;
  const rawEntry = entryOnClose ? bar.current.close : bar.next.open;
  const entryCandle = entryOnClose ? bar.current : bar.next;
  runtime.entryAttempts += 1;
  const fill = resolveEntryFill(
    rawEntry,
    entryCandle,
    side,
    runtime.feePct,
    runtime.makerFeePct,
    runtime.entryOrderType,
    runtime.entryLimitOffsetBps,
    runtime.slippageBps,
  );
  if (!fill.filled) return;
  if (fill.fillType === "maker") runtime.makerFills += 1;
  const plan = resolveBacktestEntryPlan(runtime, bar, side, fill);
  if (!plan) return;
  runtime.position = {
    entrySignal: bar.signal,
    entryPrice: plan.entryPrice,
    entryTime: entryCandle.timestamp,
    entryBarIndex: bar.index,
    side,
    size: plan.size,
    stopLoss: plan.stopLoss,
    takeProfit: plan.takeProfit,
    trailingStopAtr: plan.trailingStopAtr,
    highestPrice: plan.entryPrice,
    lowestPrice: plan.entryPrice,
    scaledOut: false,
    scaleOutPrice: plan.scaleOutPrice,
    initialRiskPct: plan.initialRiskPct,
    entryFeePaid: plan.entryFee,
    entryFeePct: plan.entryFeePct,
    fillType: plan.fillType,
  };
  runtime.capital -= plan.entryFee;
  runtime.totalFeesPaid += plan.entryFee;
  runtime.lastFundingTime = entryCandle.timestamp;
}

function processBacktestBar(
  runtime: BacktestCoreRuntime,
  bar: BacktestBarContext,
): void {
  updateBacktestSignalState(runtime, bar.signal);
  if (manageBacktestPosition(runtime, bar)) return;
  if (runtime.cooldownBarsRemaining > 0) {
    runtime.cooldownBarsRemaining--;
  }
  tryOpenBacktestPosition(runtime, bar);
}

function closeRemainingBacktestPosition(
  runtime: BacktestCoreRuntime,
  candles: readonly CandleLike[],
): void {
  const position = runtime.position;
  if (!position || candles.length === 0) return;
  const last = candles[candles.length - 1];
  const exitSide = backtestExitSide(position);
  const exitPrice = backtestExitPrice(runtime, last.close, exitSide, last);
  closeBacktestPosition(
    runtime,
    exitPrice,
    last.timestamp,
    last.timestamp,
    "signal",
  );
}

function finalizeBacktestResult(
  runtime: BacktestCoreRuntime,
  candles: readonly CandleLike[],
): BacktestResult {
  const winningTrades = runtime.trades.filter((t) => t.pnl > 0).length;
  const losingTrades = runtime.trades.filter((t) => t.pnl < 0).length;
  const totalReturnPct =
    ((runtime.capital - runtime.options.initialCapital) /
      runtime.options.initialCapital) *
    100;
  const candleSpanMs =
    candles.length > 1
      ? candles[candles.length - 1].timestamp.getTime() -
        candles[0].timestamp.getTime()
      : 0;
  const metrics = computePerformanceMetrics({
    trades: runtime.trades,
    initialCapital: runtime.options.initialCapital,
    maxDrawdownPct: runtime.maxDrawdown * 100,
    totalReturnPct,
    candleSpanMs,
  });
  const result: BacktestResult = {
    symbol: runtime.options.symbol,
    totalTrades: runtime.trades.length,
    winningTrades,
    losingTrades,
    winRate:
      runtime.trades.length > 0 ? winningTrades / runtime.trades.length : 0,
    totalReturnPct,
    maxDrawdownPct: runtime.maxDrawdown * 100,
    sharpeRatio: calculateSharpe(runtime.trades.map((t) => t.pnlPct)),
    trades: runtime.trades,
    totalFeesPaid: runtime.totalFeesPaid,
    totalFundingCost: runtime.totalFundingCost,
    benchmarkReturnPct: computeBenchmark(candles),
    metrics,
    equityCurve: runtime.equityCurve,
    makerFillRate:
      runtime.entryAttempts > 0
        ? runtime.makerFills / runtime.entryAttempts
        : undefined,
    robustnessScore: 0,
  };
  return { ...result, robustnessScore: robustnessScore(result) };
}

interface BacktestStatsContext {
  readonly statsSeries: readonly CandleLike[];
  readonly statsOffset: number;
  readonly statsForBar: (barIndex: number) => SymbolStatistics;
  readonly composerConfigFor: (barStats: SymbolStatistics) => ComposerConfig;
}

function validateBacktestCandleTimestamps(
  options: BacktestOptions,
  candles: readonly CandleLike[],
): void {
  for (let i = 0; i < candles.length; i++) {
    const ts = candles[i].timestamp;
    if (!(ts instanceof Date) || Number.isNaN(ts.getTime())) {
      throw new Error(
        `Invalid candle timestamp for ${options.symbol} at index ${i}: ` +
          `expected a valid Date, got ${String(ts)}`,
      );
    }
  }
}

function makeBacktestStatsContext(
  options: BacktestOptions,
  candles: readonly CandleLike[],
): BacktestStatsContext {
  const statsSeries = options.warmupCandles?.length
    ? [...options.warmupCandles, ...candles]
    : candles;
  const statsOffset = statsSeries.length - candles.length;
  const causalSymbolStats = options.symbolStats
    ? null
    : makeCausalSymbolStats(statsSeries, options.timeframe);
  const statsForBar = (barIndex: number): SymbolStatistics =>
    options.symbolStats ?? causalSymbolStats!(barIndex + statsOffset);
  const composerConfigFor = (barStats: SymbolStatistics): ComposerConfig => ({
    ...options.composerConfig,
    thresholds: {
      ...options.composerConfig.thresholds,
      symbolStats: barStats,
    },
  });
  return { statsSeries, statsOffset, statsForBar, composerConfigFor };
}

interface BacktestExecutionSettings {
  readonly feePct: number;
  readonly makerFeePct: number | undefined;
  readonly entryOrderType: "market" | "limit";
  readonly entryLimitOffsetBps: number;
  readonly slippageBps: number;
  readonly fundingRatePct: number;
  readonly fundingIntervalMs: number;
  readonly isFutures: boolean;
  readonly leverage: number;
}

function normalizeBacktestExecutionSettings(
  options: BacktestOptions,
): BacktestExecutionSettings {
  const isFutures = options.isFutures ?? false;
  return {
    feePct: normalizeFeePct(options.feePct),
    makerFeePct: normalizeOptionalFeePct(options.makerFeePct, "maker-fee"),
    entryOrderType: options.entryOrderType ?? "market",
    entryLimitOffsetBps: options.entryLimitOffsetBps ?? 0,
    slippageBps: options.slippageBps ?? 0,
    fundingRatePct: options.fundingRatePct ?? 0,
    fundingIntervalMs: (options.fundingIntervalHours ?? 8) * 3600_000,
    isFutures,
    leverage: isFutures ? Math.max(1, options.leverage ?? 1) : 1,
  };
}

interface BacktestRiskSettings {
  readonly breakevenAtR: number;
  readonly maxBarsInTrade: number;
  readonly lossCooldownBars: number;
  readonly signalPersistence: number;
  readonly lossConfidencePenalty: number;
  readonly lossConfidenceDecay: number;
}

function normalizeBacktestRiskSettings(
  options: BacktestOptions,
): BacktestRiskSettings {
  return {
    breakevenAtR: Math.max(0, options.breakevenAtR ?? 0),
    maxBarsInTrade: Math.max(0, Math.floor(options.maxBarsInTrade ?? 0)),
    lossCooldownBars: Math.max(0, Math.floor(options.lossCooldownBars ?? 0)),
    signalPersistence: Math.max(0, Math.floor(options.signalPersistence ?? 0)),
    lossConfidencePenalty: options.lossConfidencePenalty ?? 0,
    lossConfidenceDecay: options.lossConfidenceDecay ?? 0,
  };
}

interface BacktestFilterSettings {
  readonly sessionStart: string;
  readonly sessionEnd: string;
  readonly autoRegimeFilter: boolean;
  readonly autoRegimeAdxThreshold: number;
  readonly useObservedPrice: boolean;
}

function normalizeBacktestFilterSettings(
  options: BacktestOptions,
): BacktestFilterSettings {
  return {
    sessionStart: options.sessionStart ?? "",
    sessionEnd: options.sessionEnd ?? "",
    autoRegimeFilter: options.autoRegimeFilter ?? false,
    autoRegimeAdxThreshold: Math.max(0, options.autoRegimeAdxThreshold ?? 25),
    useObservedPrice: options.useObservedPrice ?? false,
  };
}

function createBacktestRuntime(options: BacktestOptions): BacktestCoreRuntime {
  const execution = normalizeBacktestExecutionSettings(options);
  const risk = normalizeBacktestRiskSettings(options);
  const filters = normalizeBacktestFilterSettings(options);
  return {
    options,
    ...execution,
    ...risk,
    ...filters,
    trades: [],
    equityCurve: options.recordEquityCurve ? [] : undefined,
    position: null,
    capital: options.initialCapital,
    peakCapital: options.initialCapital,
    maxDrawdown: 0,
    tradeId: 0,
    totalFeesPaid: 0,
    totalFundingCost: 0,
    lastFundingTime: null,
    entryAttempts: 0,
    makerFills: 0,
    priorSignalDirection: "hold",
    signalStreak: 0,
    lossPenalty: 0,
    cooldownBarsRemaining: 0,
  };
}

/**
 * Internal engine: run a single backtest on the provided candles.
 */
function runBacktestCore(options: BacktestOptions): BacktestResult {
  const candles = options.candles;
  if (candles.length < 20) {
    return emptyResult(options.symbol);
  }

  // Fail fast before a malformed timestamp can poison funding and metrics.
  validateBacktestCandleTimestamps(options, candles);

  // Causal per-bar symbol stats: each bar's signal and exit levels must see
  // only data available at that bar. A single whole-series stats object is
  // look-ahead bias (bd clever-cabin-dt8). The explicit options.symbolStats
  // override stays static — it exists for tests needing a fixed fixture.
  //
  // `warmupCandles` (set by the OOS split path) prepend history so BOTH the
  // causal symbol stats AND the indicator windows at every trading bar match a
  // continuous full-period run. `statsSeries` is the warmup-augmented series
  // and `statsOffset` maps a trading-bar index `i` (into `candles`) to its
  // position in `statsSeries` (bd clever-cabin-dt8). A run without warmup has
  // statsOffset=0 and behaves exactly as before — the baseline is preserved.
  const stats = makeBacktestStatsContext(options, candles);

  const runtime = createBacktestRuntime(options);

  // We need a rolling window of candles plus current OB metrics.
  // For backtesting, derive synthetic order-book metrics from the candle.
  // The indicator window is read from the warmup-augmented series at
  // `augIndex` so a split (OOS) run sees the same warmed indicators a
  // continuous full-period run would (bd clever-cabin-dt8). Trades still only
  // execute on the actual `candles` (`current`/`next`), never on warmup bars.
  for (let i = 0; i < candles.length - 1; i++) {
    const augIndex = i + stats.statsOffset;
    if (augIndex < 20) continue;
    const window = stats.statsSeries.slice(
      Math.max(0, augIndex + 1 - 200),
      augIndex + 1,
    );
    const current = candles[i];
    const next = candles[i + 1];

    const obMetrics = syntheticOrderBook(current);
    const ohlcv: OHLCVInput = {
      exchange: options.exchange,
      symbol: options.symbol,
      timeframe: options.timeframe,
      candles: window,
      fundingRates: options.fundingRates,
    };

    const symbolStats = stats.statsForBar(i);
    const signal = composeSignal(
      ohlcv,
      obMetrics,
      stats.composerConfigFor(symbolStats),
    );
    const bar: BacktestBarContext = {
      index: i,
      current,
      next,
      window,
      signal,
      symbolStats,
    };
    processBacktestBar(runtime, bar);
  }

  closeRemainingBacktestPosition(runtime, candles);
  return finalizeBacktestResult(runtime, candles);
}

function emptyResult(symbol: string): BacktestResult {
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

function syntheticOrderBook(candle: CandleLike): OrderBookMetricsInput {
  const spread = candle.high - candle.low;
  const midPrice = candle.close;
  const spreadPercent = midPrice > 0 ? spread / midPrice : 0;
  const range = candle.high - candle.low;
  const bidDepth = range > 0 ? ((candle.close - candle.low) / range) * 100 : 50;
  const askDepth =
    range > 0 ? ((candle.high - candle.close) / range) * 100 : 50;
  const imbalance =
    bidDepth + askDepth > 0 ? (bidDepth - askDepth) / (bidDepth + askDepth) : 0;

  return {
    exchange: "synthetic",
    symbol: "synthetic",
    spread,
    spreadPercent,
    bidDepth,
    askDepth,
    imbalance,
    midPrice,
    timestamp: candle.timestamp,
  };
}

function alignsWithHigherTimeframeTrend(
  side: "long" | "short",
  htfCandles: readonly CandleLike[],
  fastPeriod = 50,
  slowPeriod = 100,
): boolean {
  if (htfCandles.length < slowPeriod + 1) return true;
  const closes = htfCandles.map((c) => c.close);
  const fastEMA = calculateEMA(closes, fastPeriod);
  const slowEMA = calculateEMA(closes, slowPeriod);
  const lastFast = fastEMA[fastEMA.length - 1];
  const lastSlow = slowEMA[slowEMA.length - 1];
  if (Number.isNaN(lastFast) || Number.isNaN(lastSlow) || lastSlow === 0) {
    return true;
  }
  const htfUp = lastFast > lastSlow;
  return side === "long" ? htfUp : !htfUp;
}

function efficiencyRatio(candles: readonly CandleLike[], period = 20): number {
  if (candles.length < period + 1) return 0;
  const start = candles[candles.length - period - 1].close;
  const end = candles[candles.length - 1].close;
  if (start === 0) return 0;
  const netChange = Math.abs(end - start);
  let sumChanges = 0;
  for (let i = candles.length - period; i < candles.length; i++) {
    sumChanges += Math.abs(candles[i].close - candles[i - 1].close);
  }
  return sumChanges === 0 ? 0 : netChange / sumChanges;
}

function passesRsiNeutralZone(
  candles: readonly CandleLike[],
  side: "long" | "short",
  rsiLongMax: number,
  rsiShortMin: number,
): boolean {
  const rsi = calculateRSI(candles, 14);
  if (rsi === null) return true;
  if (side === "long") return rsi <= rsiLongMax;
  return rsi >= rsiShortMin;
}

function passesBollingerPullback(
  candles: readonly CandleLike[],
  side: "long" | "short",
  longMaxPctB: number,
  shortMinPctB: number,
): boolean {
  const bb = calculateBollingerBands(candles, 20);
  if (bb === null) return true;
  if (side === "long") return bb.percentB <= longMaxPctB;
  return bb.percentB >= shortMinPctB;
}

function priceWithinEmaPullback(
  price: number,
  candles: readonly CandleLike[],
  period: number,
  marginPct: number,
): boolean {
  if (candles.length < period + 1 || price <= 0) return true;
  const closes = candles.map((c) => c.close);
  const emaSeries = calculateEMA(closes, period);
  const ema = emaSeries[emaSeries.length - 1];
  if (Number.isNaN(ema) || ema <= 0) return true;
  const margin = marginPct / 100;
  return price >= ema * (1 - margin) && price <= ema * (1 + margin);
}

export function calculatePositionValue(
  capital: number,
  entryPrice: number,
  stopDistancePct: number,
  currentVolatility: number,
  options: Pick<
    BacktestOptions,
    | "positionSizePct"
    | "riskPerTradePct"
    | "maxPositionSizePct"
    | "leverage"
    | "isFutures"
    | "volatilityTargetAnnualPct"
  >,
): number {
  const leverage = (options.isFutures ? options.leverage : undefined) ?? 1;
  const maxPositionValue =
    capital * ((options.maxPositionSizePct ?? 100) / 100) * leverage;

  let baseValue: number;
  if (
    options.riskPerTradePct &&
    options.riskPerTradePct > 0 &&
    stopDistancePct > 0
  ) {
    const riskAmount = capital * (options.riskPerTradePct / 100);
    baseValue = (riskAmount / stopDistancePct) * leverage;
  } else {
    baseValue = capital * (options.positionSizePct / 100) * leverage;
  }

  const target = options.volatilityTargetAnnualPct ?? 0;
  if (target > 0 && currentVolatility > 0) {
    baseValue *= target / currentVolatility;
  }

  return Math.min(baseValue, maxPositionValue);
}

function checkScaleOut(
  position: BacktestPosition,
  candle: CandleLike,
  useObservedPrice: boolean,
): boolean {
  if (position.scaledOut || position.scaleOutPrice <= 0) return false;
  if (position.side === "long") {
    return useObservedPrice
      ? candle.close >= position.scaleOutPrice
      : candle.high >= position.scaleOutPrice;
  }
  return useObservedPrice
    ? candle.close <= position.scaleOutPrice
    : candle.low <= position.scaleOutPrice;
}

function checkExitLevels(
  position: BacktestPosition,
  candle: CandleLike,
  useObservedPrice: boolean,
): { stopLoss: boolean; takeProfit: boolean } {
  if (position.side === "long") {
    return useObservedPrice
      ? {
          stopLoss: candle.close <= position.stopLoss,
          takeProfit: candle.close >= position.takeProfit,
        }
      : {
          stopLoss: candle.low <= position.stopLoss,
          takeProfit: candle.high >= position.takeProfit,
        };
  }
  return useObservedPrice
    ? {
        stopLoss: candle.close >= position.stopLoss,
        takeProfit: candle.close <= position.takeProfit,
      }
    : {
        stopLoss: candle.high >= position.stopLoss,
        takeProfit: candle.low <= position.takeProfit,
      };
}

function updateTrailingStop(
  position: BacktestPosition,
  candle: CandleLike,
  options: BacktestOptions,
  useObservedPrice: boolean,
): void {
  if (!options.trailingStopPct && !options.trailingStopAtrMultiplier) return;

  if (position.side === "long") {
    const price = useObservedPrice ? candle.close : candle.high;
    if (price > position.highestPrice) {
      position.highestPrice = price;
    }
  } else {
    const price = useObservedPrice ? candle.close : candle.low;
    if (price < position.lowestPrice) {
      position.lowestPrice = price;
    }
  }

  let trailDistance: number | null = null;
  if (options.trailingStopPct && options.trailingStopPct > 0) {
    const referencePrice =
      position.side === "long" ? position.highestPrice : position.lowestPrice;
    trailDistance = referencePrice * (options.trailingStopPct / 100);
  } else if (
    options.trailingStopAtrMultiplier &&
    options.trailingStopAtrMultiplier > 0 &&
    position.trailingStopAtr
  ) {
    trailDistance =
      position.trailingStopAtr * options.trailingStopAtrMultiplier;
  }

  if (trailDistance === null || trailDistance <= 0) return;

  if (position.side === "long") {
    const candidateStop = position.highestPrice - trailDistance;
    if (candidateStop > position.stopLoss) {
      position.stopLoss = candidateStop;
    }
  } else {
    const candidateStop = position.lowestPrice + trailDistance;
    if (candidateStop < position.stopLoss) {
      position.stopLoss = candidateStop;
    }
  }
}

function applyBreakevenStop(
  position: BacktestPosition,
  price: number,
  breakevenAtR: number,
): void {
  const stopDistance = Math.abs(position.entryPrice - position.stopLoss);
  if (stopDistance <= 0 || price <= 0) return;

  if (position.side === "long") {
    const profit = price - position.entryPrice;
    if (profit >= breakevenAtR * stopDistance) {
      position.stopLoss = Math.max(position.stopLoss, position.entryPrice);
    }
  } else {
    const profit = position.entryPrice - price;
    if (profit >= breakevenAtR * stopDistance) {
      position.stopLoss = Math.min(position.stopLoss, position.entryPrice);
    }
  }
}

function isLiquidated(
  position: BacktestPosition,
  price: number,
  leverage: number,
): boolean {
  if (leverage <= 1 || price <= 0 || position.entryPrice <= 0) return false;
  const liquidationMove = 1 / leverage;
  if (position.side === "long") {
    return (
      (position.entryPrice - price) / position.entryPrice >= liquidationMove
    );
  }
  return (price - position.entryPrice) / position.entryPrice >= liquidationMove;
}

function shouldExitPosition(
  position: BacktestPosition,
  signal: ScalpingSignal,
): boolean {
  return (
    (position.side === "long" && signal.direction === "sell") ||
    (position.side === "short" && signal.direction === "buy")
  );
}

function isWithinSession(
  timestamp: Date,
  sessionStart: string,
  sessionEnd: string,
): boolean {
  if (!sessionStart || !sessionEnd) return true;
  const start = parseSessionTime(sessionStart);
  const end = parseSessionTime(sessionEnd);
  if (start === null || end === null) return true;
  const minutes = timestamp.getUTCHours() * 60 + timestamp.getUTCMinutes();
  if (start === end) return true;
  if (start < end) {
    return minutes >= start && minutes <= end;
  }
  return minutes >= start || minutes <= end;
}

function parseSessionTime(value: string): number | null {
  const match = value.match(/^(\d{1,2}):(\d{2})$/);
  if (!match) return null;
  const hours = Number.parseInt(match[1], 10);
  const minutes = Number.parseInt(match[2], 10);
  if (hours < 0 || hours > 23 || minutes < 0 || minutes > 59) return null;
  return hours * 60 + minutes;
}

function passesAutoRegimeFilter(
  candles: readonly CandleLike[],
  side: "long" | "short",
  adxThreshold: number,
): boolean {
  const adxResult = calculateADX(candles, 14);
  const adx = adxResult.adx;
  if (adx === null) return true;

  const closes = candles.map((c) => c.close);
  const emaSeries = calculateEMA(closes, 20);
  const ema = emaSeries[emaSeries.length - 1];

  if (adx >= adxThreshold) {
    // Trending regime: only enter in the direction of the EMA slope.
    if (Number.isNaN(ema) || ema <= 0) return true;
    const priorEma = emaSeries[emaSeries.length - 2];
    const slope = priorEma && !Number.isNaN(priorEma) ? ema - priorEma : 0;
    if (side === "long") return slope > 0;
    return slope < 0;
  }

  // Ranging regime: only enter on mean-reversion extremes.
  const rsi = calculateRSI(candles, 14);
  const bb = calculateBollingerBands(candles, 20);
  let longExtreme = false;
  let shortExtreme = false;
  if (rsi !== null) {
    longExtreme = rsi <= 40;
    shortExtreme = rsi >= 60;
  }
  if (bb !== null) {
    longExtreme = longExtreme || bb.percentB <= 0.2;
    shortExtreme = shortExtreme || bb.percentB >= 0.8;
  }
  return side === "long" ? longExtreme : shortExtreme;
}

function isEntrySignal(signal: ScalpingSignal, minConfidence: number): boolean {
  return signal.direction !== "hold" && signal.confidence >= minConfidence;
}

function calculatePnl(position: BacktestPosition, exitPrice: number): number {
  const priceDiff =
    position.side === "long"
      ? exitPrice - position.entryPrice
      : position.entryPrice - exitPrice;
  return priceDiff * position.size;
}

export function calculateSharpe(returns: readonly number[]): number {
  if (returns.length < 2) return 0;
  const mean = returns.reduce((a, b) => a + b, 0) / returns.length;
  const variance =
    returns.reduce((sum, r) => sum + (r - mean) ** 2, 0) / (returns.length - 1);
  const std = Math.sqrt(variance);
  return std === 0 ? 0 : mean / std;
}

function applySlippage(
  price: number,
  side: "long" | "short",
  bps: number,
  candleHigh: number,
  candleLow: number,
): number {
  if (bps === 0) return price;
  const factor = bps / 10000;
  const slipped = side === "long" ? price * (1 + factor) : price * (1 - factor);
  return Math.min(candleHigh, Math.max(candleLow, slipped));
}

function applyObservedSlippage(
  close: number,
  side: "long" | "short",
  bps: number,
): number {
  if (bps === 0) return close;
  const factor = bps / 10000;
  return side === "long" ? close * (1 + factor) : close * (1 - factor);
}

function computeBenchmark(candles: readonly CandleLike[]): number {
  if (candles.length < 2) return 0;
  const first = candles[0].close;
  const last = candles[candles.length - 1].close;
  return first === 0 ? 0 : ((last - first) / first) * 100;
}

interface FundingCharge {
  readonly funding: number;
  readonly newLastFundingTime: Date;
}

function chargeFunding(
  position: BacktestPosition,
  lastFundingTime: Date,
  now: Date,
  fundingRatePct: number,
  fundingIntervalMs: number,
  isFutures: boolean,
  fundingRates?: readonly FundingRate[],
): FundingCharge {
  if (!isFutures) {
    return { funding: 0, newLastFundingTime: lastFundingTime };
  }
  const elapsed = now.getTime() - lastFundingTime.getTime();
  if (elapsed < fundingIntervalMs) {
    return { funding: 0, newLastFundingTime: lastFundingTime };
  }
  const intervals = Math.floor(elapsed / fundingIntervalMs);
  const notional = position.entryPrice * position.size;
  // When historical rates are supplied, accrue the REAL per-interval rates:
  // the mean of the rates whose timestamps fall inside the charged window
  // (lastFundingTime, now], applied at each funding interval. FundingRate is
  // a decimal (0.0001 = 0.01%/8h) — multiply by 100 to match the pct units
  // of fundingRatePct (0.01 = 0.01%). Falls back to the flat constant when
  // no rates are supplied or none fall inside the window.
  let ratePct = fundingRatePct;
  if (fundingRates && fundingRates.length > 0) {
    const from = lastFundingTime.getTime();
    const to = now.getTime();
    let sum = 0;
    let count = 0;
    for (const rate of fundingRates) {
      const rateTime = rate.timestamp.getTime();
      if (rateTime > from && rateTime <= to) {
        sum += rate.fundingRate * 100;
        count += 1;
      }
    }
    if (count > 0) ratePct = sum / count;
  }
  if (ratePct === 0) {
    return { funding: 0, newLastFundingTime: lastFundingTime };
  }
  const effectiveRate = position.side === "long" ? ratePct : -ratePct;
  const funding = notional * (effectiveRate / 100) * intervals;
  const newLastFundingTime = new Date(
    lastFundingTime.getTime() + intervals * fundingIntervalMs,
  );
  return { funding, newLastFundingTime };
}

export interface SplitCandles {
  readonly is: readonly CandleLike[];
  readonly oos: readonly CandleLike[];
}

export function splitCandlesByOos(
  candles: readonly CandleLike[],
  oosPct: number,
): SplitCandles {
  const sorted = [...candles].sort(
    (a, b) => a.timestamp.getTime() - b.timestamp.getTime(),
  );
  const cutIndex = Math.max(
    1,
    Math.min(sorted.length - 1, Math.floor(sorted.length * (1 - oosPct / 100))),
  );
  return { is: sorted.slice(0, cutIndex), oos: sorted.slice(cutIndex) };
}

function runMonteCarlo(
  trades: readonly BacktestTrade[],
  initialCapital: number,
  iterations: number,
): MonteCarloResult {
  const rng = mulberry32(12345);
  const maxDrawdowns: number[] = [];
  let ruinCount = 0;
  let worstMaxDrawdown = 0;

  for (let i = 0; i < iterations; i++) {
    const shuffled = shuffle([...trades], rng);
    let capital = initialCapital;
    let peak = capital;
    let maxDd = 0;
    let ruined = false;
    for (const trade of shuffled) {
      capital += trade.netPnl;
      if (capital <= 0) {
        ruined = true;
        break;
      }
      if (capital > peak) peak = capital;
      if (peak > 0) {
        const dd = ((peak - capital) / peak) * 100;
        if (dd > maxDd) maxDd = dd;
      }
    }
    if (ruined || capital <= 0) ruinCount++;
    maxDrawdowns.push(maxDd);
    if (maxDd > worstMaxDrawdown) worstMaxDrawdown = maxDd;
  }

  const sorted = [...maxDrawdowns].sort((a, b) => a - b);
  return {
    iterations,
    medianMaxDrawdownPct: percentile(sorted, 0.5),
    p95MaxDrawdownPct: percentile(sorted, 0.95),
    p99MaxDrawdownPct: percentile(sorted, 0.99),
    worstMaxDrawdownPct: worstMaxDrawdown,
    probabilityOfRuinPct: (ruinCount / iterations) * 100,
  };
}

function percentile(sortedAsc: readonly number[], q: number): number {
  if (sortedAsc.length === 0) return 0;
  const index = Math.max(0, Math.ceil(sortedAsc.length * q) - 1);
  return sortedAsc[index];
}

function shuffle<T>(array: T[], rng: () => number): T[] {
  for (let i = array.length - 1; i > 0; i--) {
    const j = Math.floor(rng() * (i + 1));
    [array[i], array[j]] = [array[j], array[i]];
  }
  return array;
}

function mulberry32(seed: number): () => number {
  let t = seed >>> 0;
  return () => {
    t += 0x6d2b79f5;
    let r = Math.imul(t ^ (t >>> 15), t | 1);
    r ^= r + Math.imul(r ^ (r >>> 7), r | 61);
    return ((r ^ (r >>> 14)) >>> 0) / 4294967296;
  };
}
