import type {
  CandleLike,
  ComposerConfig,
  Direction,
  FundingRate,
  OHLCVInput,
  OrderBookMetricsInput,
  ScalpingSignal,
} from "./types.js";
import { composeSignal } from "./composer.js";
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

export function resolveEntryFill(
  rawEntry: number,
  next: CandleLike,
  side: "long" | "short",
  feePct: number,
  makerFeePct: number | undefined,
  entryOrderType: "market" | "limit",
  entryLimitOffsetBps: number,
  slippageBps: number,
): {
  entryPrice: number;
  appliedFeePct: number;
  fillType: "maker" | "taker";
  filled: boolean;
} {
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

/**
 * Assess whether a backtest result looks too good to be true.
 */
export function assessBacktestRealism(
  result: BacktestResult,
  options: {
    readonly entryOrderType?: "market" | "limit";
    readonly strict?: boolean;
  } = {},
): { ok: boolean; errors: string[]; warnings: string[] } {
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

/**
 * Internal engine: run a single backtest on the provided candles.
 */
function runBacktestCore(options: BacktestOptions): BacktestResult {
  const candles = options.candles;
  if (candles.length < 20) {
    return emptyResult(options.symbol);
  }

  // Fail fast on bad timestamps: an Invalid Date (or a non-Date) silently
  // poisons funding accrual, duration metrics, and trade-level analysis
  // (entryTime/exitTime serialize as null), which is far worse than a throw.
  for (let i = 0; i < candles.length; i++) {
    const ts = candles[i].timestamp;
    if (!(ts instanceof Date) || Number.isNaN(ts.getTime())) {
      throw new Error(
        `Invalid candle timestamp for ${options.symbol} at index ${i}: ` +
          `expected a valid Date, got ${String(ts)}`,
      );
    }
  }

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

  const feePct = normalizeFeePct(options.feePct);
  const makerFeePct = normalizeOptionalFeePct(options.makerFeePct, "maker-fee");
  const entryOrderType = options.entryOrderType ?? "market";
  const entryLimitOffsetBps = options.entryLimitOffsetBps ?? 0;

  const trades: BacktestTrade[] = [];
  let equityCurve: BacktestEquityPoint[] | undefined = options.recordEquityCurve
    ? []
    : undefined;
  const recordEquityPoint = (timestamp: Date) => {
    if (equityCurve) {
      equityCurve.push({
        tradeIndex: trades.length - 1,
        timestamp,
        capital,
      });
    }
  };
  let position: BacktestPosition | null = null;
  let capital = options.initialCapital;
  let peakCapital = capital;
  let maxDrawdown = 0;
  let tradeId = 0;
  let totalFeesPaid = 0;
  let totalFundingCost = 0;
  let lastFundingTime: Date | null = null;
  let entryAttempts = 0;
  let makerFills = 0;
  let priorSignalDirection: Direction = "hold";
  let signalStreak = 0;
  const signalPersistence = Math.max(
    0,
    Math.floor(options.signalPersistence ?? 0),
  );
  let lossPenalty = 0;
  const lossConfidencePenalty = options.lossConfidencePenalty ?? 0;
  const lossConfidenceDecay = options.lossConfidenceDecay ?? 0;

  const slippageBps = options.slippageBps ?? 0;
  const fundingRatePct = options.fundingRatePct ?? 0;
  const fundingIntervalMs = (options.fundingIntervalHours ?? 8) * 3600_000;
  const isFutures = options.isFutures ?? false;
  const leverage = isFutures ? Math.max(1, options.leverage ?? 1) : 1;
  const breakevenAtR = Math.max(0, options.breakevenAtR ?? 0);
  const maxBarsInTrade = Math.max(0, Math.floor(options.maxBarsInTrade ?? 0));
  const lossCooldownBars = Math.max(
    0,
    Math.floor(options.lossCooldownBars ?? 0),
  );
  let cooldownBarsRemaining = 0;
  const sessionStart = options.sessionStart ?? "";
  const sessionEnd = options.sessionEnd ?? "";
  const autoRegimeFilter = options.autoRegimeFilter ?? false;
  const autoRegimeAdxThreshold = Math.max(
    0,
    options.autoRegimeAdxThreshold ?? 25,
  );
  const useObservedPrice = options.useObservedPrice ?? false;

  // We need a rolling window of candles plus current OB metrics.
  // For backtesting, derive synthetic order-book metrics from the candle.
  // The indicator window is read from the warmup-augmented series at
  // `augIndex` so a split (OOS) run sees the same warmed indicators a
  // continuous full-period run would (bd clever-cabin-dt8). Trades still only
  // execute on the actual `candles` (`current`/`next`), never on warmup bars.
  for (let i = 0; i < candles.length - 1; i++) {
    const augIndex = i + statsOffset;
    if (augIndex < 20) continue;
    const window = statsSeries.slice(
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

    const symbolStats = statsForBar(i);
    const signal = composeSignal(
      ohlcv,
      obMetrics,
      composerConfigFor(symbolStats),
    );
    const currentDirection = signal?.direction ?? "hold";
    if (
      currentDirection !== "hold" &&
      currentDirection === priorSignalDirection
    ) {
      signalStreak += 1;
    } else {
      signalStreak = currentDirection === "hold" ? 0 : 1;
    }
    priorSignalDirection = currentDirection;
    if (lossPenalty > 0 && lossConfidenceDecay > 0) {
      lossPenalty = Math.max(0, lossPenalty - lossConfidenceDecay);
    }

    // Update running capital/drawdown if position open
    if (position) {
      const mtmPrice = current.close;
      const mtmPnl = calculatePnl(position, mtmPrice);
      const mtmCapital = capital + mtmPnl;
      if (mtmCapital > peakCapital) peakCapital = mtmCapital;
      const drawdown = (peakCapital - mtmCapital) / peakCapital;
      if (drawdown > maxDrawdown) maxDrawdown = drawdown;

      // Update trailing stop based on favorable price movement.
      updateTrailingStop(position, current, options, useObservedPrice);

      // Move stop-loss to breakeven once the trade has reached +R profit.
      if (breakevenAtR > 0) {
        applyBreakevenStop(position, current.close, breakevenAtR);
      }

      // Liquidation check for leveraged futures.
      if (leverage > 1 && isLiquidated(position, current.close, leverage)) {
        const exitSide = position.side === "long" ? "short" : "long";
        const exitPrice = useObservedPrice
          ? applyObservedSlippage(current.close, exitSide, slippageBps)
          : applySlippage(
              current.close,
              exitSide,
              slippageBps,
              current.high,
              current.low,
            );
        const notional = position.entryPrice * position.size;
        const pnl = -notional / leverage;
        const exitFee = exitPrice * position.size * (feePct / 100);
        const entryFee = position.entryFeePaid;
        const funding = chargeFunding(
          position,
          lastFundingTime!,
          current.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
          options.fundingRates,
        );
        const pnlPct = ((pnl - exitFee) / notional) * 100;
        capital += pnl - exitFee - funding.funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding.funding;
        lastFundingTime = funding.newLastFundingTime;
        trades.push({
          id: `trade-${tradeId++}`,
          symbol: options.symbol,
          side: position.side,
          entryTime: position.entryTime,
          exitTime: current.timestamp,
          entryPrice: position.entryPrice,
          exitPrice,
          pnl,
          pnlPct,
          netPnl: pnl - exitFee - entryFee - funding.funding,
          exitReason: "liquidation",
          initialRiskPct: position.initialRiskPct,
          fillType: position.fillType,
          entryFeePct: position.entryFeePct,
          exitFeePct: feePct,
        });
        recordEquityPoint(current.timestamp);
        if (pnl - exitFee - funding.funding < 0) {
          lossPenalty = lossConfidencePenalty;
          cooldownBarsRemaining = lossCooldownBars;
        }
        position = null;
        lastFundingTime = null;
        continue;
      }

      // Time-stop: close position after max allowed bars.
      if (
        maxBarsInTrade > 0 &&
        i - position.entryBarIndex + 1 >= maxBarsInTrade
      ) {
        const exitSide = position.side === "long" ? "short" : "long";
        const exitPrice = useObservedPrice
          ? applyObservedSlippage(current.close, exitSide, slippageBps)
          : applySlippage(
              current.close,
              exitSide,
              slippageBps,
              current.high,
              current.low,
            );
        const pnl = calculatePnl(position, exitPrice);
        const exitFee = exitPrice * position.size * (feePct / 100);
        const entryFee = position.entryFeePaid;
        const funding = chargeFunding(
          position,
          lastFundingTime!,
          current.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
          options.fundingRates,
        );
        const pnlPct =
          ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
        capital += pnl - exitFee - funding.funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding.funding;
        lastFundingTime = funding.newLastFundingTime;
        trades.push({
          id: `trade-${tradeId++}`,
          symbol: options.symbol,
          side: position.side,
          entryTime: position.entryTime,
          exitTime: current.timestamp,
          entryPrice: position.entryPrice,
          exitPrice,
          pnl,
          pnlPct,
          netPnl: pnl - exitFee - entryFee - funding.funding,
          exitReason: "time_stop",
          initialRiskPct: position.initialRiskPct,
          fillType: position.fillType,
          entryFeePct: position.entryFeePct,
          exitFeePct: feePct,
        });
        recordEquityPoint(current.timestamp);
        if (pnl - exitFee - funding.funding < 0) {
          lossPenalty = lossConfidencePenalty;
          cooldownBarsRemaining = lossCooldownBars;
        }
        position = null;
        lastFundingTime = null;
        continue;
      }

      // Check partial scale-out before stop/take-profit.
      if (checkScaleOut(position, current, useObservedPrice)) {
        const scaleOutPct = Math.max(
          0,
          Math.min(100, options.scaleOutPct ?? 50),
        );
        const partialSize = position.size * (scaleOutPct / 100);
        if (partialSize > 0) {
          const exitSide = position.side === "long" ? "short" : "long";
          const exitPrice = useObservedPrice
            ? applyObservedSlippage(current.close, exitSide, slippageBps)
            : applySlippage(
                position.scaleOutPrice,
                exitSide,
                slippageBps,
                current.high,
                current.low,
              );
          const pnl = calculatePnl(
            { ...position, size: partialSize },
            exitPrice,
          );
          const exitFee = exitPrice * partialSize * (feePct / 100);
          const entryFee =
            position.size > 0
              ? position.entryFeePaid * (partialSize / position.size)
              : 0;
          const pnlPct =
            ((pnl - exitFee) / (position.entryPrice * partialSize)) * 100;
          capital += pnl - exitFee;
          totalFeesPaid += exitFee;
          trades.push({
            id: `trade-${tradeId++}`,
            symbol: options.symbol,
            side: position.side,
            entryTime: position.entryTime,
            exitTime: current.timestamp,
            entryPrice: position.entryPrice,
            exitPrice,
            pnl,
            pnlPct,
            netPnl: pnl - exitFee - entryFee,
            exitReason: "scale_out",
            initialRiskPct: position.initialRiskPct,
            fillType: position.fillType,
            entryFeePct: position.entryFeePct,
            exitFeePct: feePct,
          });
          recordEquityPoint(current.timestamp);
          position.size -= partialSize;
          position.stopLoss = position.entryPrice;
          position.scaledOut = true;
          if (pnl - exitFee < 0) {
            cooldownBarsRemaining = lossCooldownBars;
          }
        }
        continue;
      }

      // Check stop-loss / take-profit on current candle
      const { stopLoss, takeProfit } = checkExitLevels(
        position,
        current,
        useObservedPrice,
      );
      if (stopLoss || takeProfit) {
        const exitSide = position.side === "long" ? "short" : "long";
        const exitPrice = useObservedPrice
          ? applyObservedSlippage(current.close, exitSide, slippageBps)
          : applySlippage(
              stopLoss ? position.stopLoss : position.takeProfit,
              exitSide,
              slippageBps,
              current.high,
              current.low,
            );
        const reason = stopLoss ? "stop_loss" : "take_profit";
        const pnl = calculatePnl(position, exitPrice);
        const exitFee = exitPrice * position.size * (feePct / 100);
        const entryFee = position.entryFeePaid;
        const funding = chargeFunding(
          position,
          lastFundingTime!,
          current.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
          options.fundingRates,
        );
        const pnlPct =
          ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
        capital += pnl - exitFee - funding.funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding.funding;
        lastFundingTime = funding.newLastFundingTime;
        trades.push({
          id: `trade-${tradeId++}`,
          symbol: options.symbol,
          side: position.side,
          entryTime: position.entryTime,
          exitTime: current.timestamp,
          entryPrice: position.entryPrice,
          exitPrice,
          pnl,
          pnlPct,
          netPnl: pnl - exitFee - entryFee - funding.funding,
          exitReason: reason,
          initialRiskPct: position.initialRiskPct,
          fillType: position.fillType,
          entryFeePct: position.entryFeePct,
          exitFeePct: feePct,
        });
        recordEquityPoint(current.timestamp);
        if (pnl - exitFee - funding.funding < 0) {
          lossPenalty = lossConfidencePenalty;
          cooldownBarsRemaining = lossCooldownBars;
        }
        position = null;
        lastFundingTime = null;
        continue;
      }

      // RSI normalization exit.
      if (
        checkRsiExit({
          side: position.side,
          candles: window,
          exitRsiPeriod: options.exitRsiPeriod,
          exitRsiLongLevel: options.exitRsiLongLevel,
          exitRsiShortLevel: options.exitRsiShortLevel,
        })
      ) {
        const exitSide = position.side === "long" ? "short" : "long";
        const exitPrice = useObservedPrice
          ? applyObservedSlippage(current.close, exitSide, slippageBps)
          : applySlippage(
              next.open,
              exitSide,
              slippageBps,
              next.high,
              next.low,
            );
        const pnl = calculatePnl(position, exitPrice);
        const exitFee = exitPrice * position.size * (feePct / 100);
        const entryFee = position.entryFeePaid;
        const funding = chargeFunding(
          position,
          lastFundingTime!,
          useObservedPrice ? current.timestamp : next.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
          options.fundingRates,
        );
        const pnlPct =
          ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
        capital += pnl - exitFee - funding.funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding.funding;
        lastFundingTime = funding.newLastFundingTime;
        trades.push({
          id: `trade-${tradeId++}`,
          symbol: options.symbol,
          side: position.side,
          entryTime: position.entryTime,
          exitTime: useObservedPrice ? current.timestamp : next.timestamp,
          entryPrice: position.entryPrice,
          exitPrice,
          pnl,
          pnlPct,
          netPnl: pnl - exitFee - entryFee - funding.funding,
          exitReason: "rsi_exit",
          initialRiskPct: position.initialRiskPct,
          fillType: position.fillType,
          entryFeePct: position.entryFeePct,
          exitFeePct: feePct,
        });
        recordEquityPoint(
          useObservedPrice ? current.timestamp : next.timestamp,
        );
        if (pnl - exitFee - funding.funding < 0) {
          lossPenalty = lossConfidencePenalty;
          cooldownBarsRemaining = lossCooldownBars;
        }
        position = null;
        lastFundingTime = null;
        continue;
      }

      // Exit on signal reversal unless holding until stop/take-profit.
      if (
        !options.holdUntilStop &&
        signal &&
        shouldExitPosition(position, signal)
      ) {
        const exitSide = position.side === "long" ? "short" : "long";
        const exitPrice = applySlippage(
          next.open,
          exitSide,
          slippageBps,
          next.high,
          next.low,
        );
        const pnl = calculatePnl(position, exitPrice);
        const exitFee = exitPrice * position.size * (feePct / 100);
        const entryFee = position.entryFeePaid;
        const funding = chargeFunding(
          position,
          lastFundingTime!,
          next.timestamp,
          fundingRatePct,
          fundingIntervalMs,
          isFutures,
          options.fundingRates,
        );
        const pnlPct =
          ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
        capital += pnl - exitFee - funding.funding;
        totalFeesPaid += exitFee;
        totalFundingCost += funding.funding;
        lastFundingTime = funding.newLastFundingTime;
        trades.push({
          id: `trade-${tradeId++}`,
          symbol: options.symbol,
          side: position.side,
          entryTime: position.entryTime,
          exitTime: next.timestamp,
          entryPrice: position.entryPrice,
          exitPrice,
          pnl,
          pnlPct,
          netPnl: pnl - exitFee - entryFee - funding.funding,
          exitReason: "signal",
          initialRiskPct: position.initialRiskPct,
          fillType: position.fillType,
          entryFeePct: position.entryFeePct,
          exitFeePct: feePct,
        });
        recordEquityPoint(next.timestamp);
        if (pnl - exitFee - funding.funding < 0) {
          lossPenalty = lossConfidencePenalty;
          cooldownBarsRemaining = lossCooldownBars;
        }
        position = null;
        lastFundingTime = null;
      }
    }

    // Decrement loss cooldown before considering a new entry.
    if (cooldownBarsRemaining > 0) {
      cooldownBarsRemaining--;
    }

    // Open new position if signal is strong enough and no position
    const persistenceMet =
      signalPersistence <= 1 || signalStreak >= signalPersistence;
    const effectiveMinConfidence = options.minConfidence + lossPenalty;
    if (
      !position &&
      cooldownBarsRemaining === 0 &&
      signal &&
      isEntrySignal(signal, effectiveMinConfidence) &&
      persistenceMet &&
      isWithinSession(current.timestamp, sessionStart, sessionEnd) &&
      (!autoRegimeFilter ||
        passesAutoRegimeFilter(
          window,
          signal.direction === "buy" ? "long" : "short",
          autoRegimeAdxThreshold,
        ))
    ) {
      const side = signal.direction === "buy" ? "long" : "short";

      if (
        options.htfCandles &&
        options.htfCandles.length > 0 &&
        !alignsWithHigherTimeframeTrend(
          side,
          // Only consider HTF candles already closed by the decision time;
          // otherwise the trend reflects future information (look-ahead bias).
          options.htfCandles.filter(
            (htf) => htf.timestamp.getTime() <= current.timestamp.getTime(),
          ),
          options.htfTrendFastPeriod,
          options.htfTrendSlowPeriod,
        )
      ) {
        continue;
      }

      if (
        options.entryPullbackEmaPeriod &&
        options.entryPullbackEmaPeriod > 0 &&
        !priceWithinEmaPullback(
          current.close,
          window,
          options.entryPullbackEmaPeriod,
          options.entryPullbackMarginPct ?? 0.1,
        )
      ) {
        continue;
      }

      if (
        options.minEfficiencyRatio &&
        options.minEfficiencyRatio > 0 &&
        efficiencyRatio(window, options.efficiencyRatioPeriod ?? 20) <
          options.minEfficiencyRatio
      ) {
        continue;
      }

      if (
        (options.rsiLongMax || options.rsiShortMin) &&
        !passesRsiNeutralZone(
          window,
          side,
          options.rsiLongMax ?? 100,
          options.rsiShortMin ?? 0,
        )
      ) {
        continue;
      }

      const useBollingerLong =
        options.bollingerLongMaxPctB !== undefined &&
        options.bollingerLongMaxPctB >= 0 &&
        options.bollingerLongMaxPctB <= 1;
      const useBollingerShort =
        options.bollingerShortMinPctB !== undefined &&
        options.bollingerShortMinPctB >= 0 &&
        options.bollingerShortMinPctB <= 1;
      if (
        (useBollingerLong || useBollingerShort) &&
        !passesBollingerPullback(
          window,
          side,
          useBollingerLong ? options.bollingerLongMaxPctB! : 1,
          useBollingerShort ? options.bollingerShortMinPctB! : 0,
        )
      ) {
        continue;
      }

      const entryOnClose = options.entryOnClose ?? false;
      const rawEntry = entryOnClose ? current.close : next.open;
      const entryCandle = entryOnClose ? current : next;
      entryAttempts += 1;
      const fill = resolveEntryFill(
        rawEntry,
        entryCandle,
        side,
        feePct,
        makerFeePct,
        entryOrderType,
        entryLimitOffsetBps,
        slippageBps,
      );
      if (!fill.filled) {
        continue;
      }
      if (fill.fillType === "maker") {
        makerFills += 1;
      }
      const entryPrice = fill.entryPrice;
      const entryFeePct = fill.appliedFeePct;

      const needsAtr =
        options.useAtrStops ||
        options.trailingStopAtrMultiplier ||
        options.minAtrPct;
      const atr = needsAtr ? calculateATR(window, 14) : null;
      const useAtr = options.useAtrStops && atr !== null && atr > 0;
      const stopMult = options.atrStopMultiplier ?? 1.5;
      const tpMult = options.atrTakeProfitMultiplier ?? 2.5;

      if (options.minAtrPct && options.minAtrPct > 0) {
        const atrPct = atr && entryPrice > 0 ? atr / entryPrice : 0;
        if (atrPct < options.minAtrPct / 100) {
          continue;
        }
      }

      const atrRiskReward =
        options.atrRiskReward && options.atrRiskReward > 0
          ? options.atrRiskReward
          : tpMult / stopMult;
      const { stopLoss, takeProfit, scaleOutPrice } = computeExitLevels({
        side,
        entryPrice,
        atr,
        useAtr: !!useAtr,
        atrStopMultiplier: stopMult,
        atrRiskReward,
        stopLossPct: options.stopLossPct,
        takeProfitPct: options.takeProfitPct,
        scaleOutAtR: options.scaleOutAtR ?? 0,
        candles: window,
        volatilityLookback: options.volatilityLookback ?? 0,
        volatilityLowPct: options.volatilityLowPct ?? 20,
        volatilityHighPct: options.volatilityHighPct ?? 80,
        volatilityLowFactor: options.volatilityLowFactor ?? 0.8,
        volatilityHighFactor: options.volatilityHighFactor ?? 1.2,
        symbolStats,
        useAdaptiveStops: options.useAdaptiveStops,
        adaptiveStopAtrMultiplier: options.adaptiveStopAtrMultiplier,
        adaptiveRiskReward: options.adaptiveRiskReward,
      });

      const stopDistancePct =
        entryPrice > 0 ? Math.abs(entryPrice - stopLoss) / entryPrice : 0;
      const initialRiskPct = stopDistancePct;
      const currentVolatility = calculateAnnualizedVolatility(
        window,
        options.volatilityLookback ?? 0,
        options.timeframe,
      );
      const positionValue = calculatePositionValue(
        capital,
        entryPrice,
        stopDistancePct,
        currentVolatility,
        options,
      );
      const size = positionValue / entryPrice;

      const entryFee = positionValue * (entryFeePct / 100);
      position = {
        entrySignal: signal,
        entryPrice,
        entryTime: next.timestamp,
        entryBarIndex: i,
        side,
        size,
        stopLoss,
        takeProfit,
        trailingStopAtr: useAtr ? atr : undefined,
        highestPrice: entryPrice,
        lowestPrice: entryPrice,
        scaledOut: false,
        scaleOutPrice: scaleOutPrice ?? 0,
        initialRiskPct,
        entryFeePaid: entryFee,
        entryFeePct,
        fillType: fill.fillType,
      };

      capital -= entryFee;
      totalFeesPaid += entryFee;
      lastFundingTime = next.timestamp;
    }
  }

  // Close any open position at the last candle
  if (position && candles.length > 0) {
    const last = candles[candles.length - 1];
    const exitSide = position.side === "long" ? "short" : "long";
    const exitPrice = useObservedPrice
      ? applyObservedSlippage(last.close, exitSide, slippageBps)
      : applySlippage(last.close, exitSide, slippageBps, last.high, last.low);
    const pnl = calculatePnl(position, exitPrice);
    const exitFee = exitPrice * position.size * (feePct / 100);
    const entryFee = position.entryFeePaid;
    const funding = chargeFunding(
      position,
      lastFundingTime!,
      last.timestamp,
      fundingRatePct,
      fundingIntervalMs,
      isFutures,
      options.fundingRates,
    );
    const pnlPct =
      ((pnl - exitFee) / (position.entryPrice * position.size)) * 100;
    capital += pnl - exitFee - funding.funding;
    totalFeesPaid += exitFee;
    totalFundingCost += funding.funding;
    lastFundingTime = funding.newLastFundingTime;
    trades.push({
      id: `trade-${tradeId++}`,
      symbol: options.symbol,
      side: position.side,
      entryTime: position.entryTime,
      exitTime: last.timestamp,
      entryPrice: position.entryPrice,
      exitPrice,
      pnl,
      pnlPct,
      netPnl: pnl - exitFee - entryFee - funding.funding,
      exitReason: "signal",
      initialRiskPct: position.initialRiskPct,
      fillType: position.fillType,
      entryFeePct: position.entryFeePct,
      exitFeePct: feePct,
    });
    recordEquityPoint(last.timestamp);
    if (pnl - exitFee - funding.funding < 0) {
      lossPenalty = lossConfidencePenalty;
      cooldownBarsRemaining = lossCooldownBars;
    }
  }

  const winningTrades = trades.filter((t) => t.pnl > 0).length;
  const losingTrades = trades.filter((t) => t.pnl < 0).length;
  const totalReturnPct =
    ((capital - options.initialCapital) / options.initialCapital) * 100;
  const returns = trades.map((t) => t.pnlPct);
  const sharpe = calculateSharpe(returns);

  const candleSpanMs =
    candles.length > 1
      ? candles[candles.length - 1].timestamp.getTime() -
        candles[0].timestamp.getTime()
      : 0;

  const metrics = computePerformanceMetrics({
    trades,
    initialCapital: options.initialCapital,
    maxDrawdownPct: maxDrawdown * 100,
    totalReturnPct,
    candleSpanMs,
  });

  const result: BacktestResult = {
    symbol: options.symbol,
    totalTrades: trades.length,
    winningTrades,
    losingTrades,
    winRate: trades.length > 0 ? winningTrades / trades.length : 0,
    totalReturnPct,
    maxDrawdownPct: maxDrawdown * 100,
    sharpeRatio: sharpe,
    trades,
    totalFeesPaid,
    totalFundingCost,
    benchmarkReturnPct: computeBenchmark(candles),
    metrics,
    equityCurve,
    makerFillRate: entryAttempts > 0 ? makerFills / entryAttempts : undefined,
    robustnessScore: 0,
  };

  return {
    ...result,
    robustnessScore: robustnessScore(result),
  };
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

function chargeFunding(
  position: BacktestPosition,
  lastFundingTime: Date,
  now: Date,
  fundingRatePct: number,
  fundingIntervalMs: number,
  isFutures: boolean,
  fundingRates?: readonly FundingRate[],
): { funding: number; newLastFundingTime: Date } {
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

export function splitCandlesByOos(
  candles: readonly CandleLike[],
  oosPct: number,
): { is: readonly CandleLike[]; oos: readonly CandleLike[] } {
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
