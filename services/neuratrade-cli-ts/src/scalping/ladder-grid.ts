/**
 * Multi-Level Ladder Grid Engine.
 *
 * A market-neutral ladder grid that can hold N simultaneous rungs per side.
 * Removes the single-position engine's "one position at a time" bottleneck so
 * a single oscillation can yield up to N fills per side.
 *
 * Design (concrete rules):
 *  - Each side maintains a ladder of up to N rungs seeded from a reference
 *    `base` (the bar open when the ladder seeds).
 *    Long levels:  base - k*step ; Short levels: base + k*step, k = 1..N.
 *  - PROGRESSIVE arming: rung k is only armed once rung k-1 has filled, so a
 *    deep rung fills only after price actually traded through the shallower
 *    levels (avoids overcounting wide-wick fills).
 *  - A rung fills when a bar's low <= its level (long) / high >= its level
 *    (short) -- identical "touch = fill" to the single-position engine.
 *  - Each rung has its OWN take-profit at entry + step*targetRatio (long) /
 *    entry - step*targetRatio (short).
 *  - Ladder stop: when price breaches the ladder boundary
 *    long: low <= base - step*(N + gridMaxGrids)
 *    short: high >= base + step*(N + gridMaxGrids)
 *    so gridMaxGrids is the stop margin BEYOND the deepest rung (matches the
 *    single-position semantics: stop is gridMaxGrids steps from the entry).
 *  - The ladder goes flat when all filled rungs have closed; it then re-seeds
 *    on a later bar. pauseAfterLossBars applies after a ladder stop-out.
 *  - Optional SMA/EMA trend filter (onlyWithTrend).
 *
 * PnL accounting: each rung is sized positionFraction/N of capital, capital
 * compounds per rung close, per-rung liquidation at leverage.
 */

import type { CandleLike } from "./types.js";
import { calculateSMA, calculateEMA } from "./indicators.js";
import { makeCausalSymbolStats } from "./symbol-stats.js";
import type { SymbolStatistics } from "./symbol-stats.js";

/**
 * Causal stats (ATR/ADX series) are a pure function of the candle array +
 * timeframe. Walk-forward selection backtests the SAME train slice once per
 * search-space candidate (~768×/window), rebuilding the O(n) ADX series
 * every time. Memoize by array identity — slices are stable within a window,
 * fresh across windows, and the WeakMap lets dead slices GC.
 */
interface CausalStatsEntry {
  readonly timeframe: string;
  readonly provider: (barIndex: number) => SymbolStatistics;
}
const causalStatsCache = new WeakMap<readonly CandleLike[], CausalStatsEntry>();
function cachedCausalStats(
  candles: readonly CandleLike[],
  timeframe: string,
): (barIndex: number) => SymbolStatistics {
  const hit = causalStatsCache.get(candles);
  if (hit && hit.timeframe === timeframe) return hit.provider;
  const provider = makeCausalSymbolStats(candles, timeframe);
  causalStatsCache.set(candles, { timeframe, provider });
  return provider;
}
export interface LadderOptions {
  /** Number of simultaneous rungs per side (1 = single-position control). */
  readonly rungs: number;
  /** Spacing between rungs as % of the reference price (e.g. 0.4 = 0.4%). */
  readonly gridStepPct: number;
  /** Stop margin (in steps) BEYOND the deepest rung. */
  readonly gridMaxGrids: number;
  /** Bars to pause after a losing stop-out (0 = no pause). */
  readonly gridPauseAfterLossBars: number;
  /** Round-trip fee percent (maker each side, symmetric baseline). */
  readonly feePct: number;
  /** Slippage in basis points applied to limit fills. */
  readonly slippageBps: number;
  /** Initial capital in quote currency. */
  readonly initialCapital: number;
  /** Leverage multiplier. 1 = spot-style (no liquidation). */
  readonly leverage: number;
  /** Trend filter period; 0 disables. */
  readonly trendFilterPeriod: number;
  /** Trend filter type: "sma" (default) or "ema". */
  readonly trendFilterType?: "sma" | "ema";
  /** Only enter long above / short below the trend filter. */
  readonly onlyWithTrend?: boolean;
  /** TP distance as multiple of grid step (default 1.0). */
  readonly targetRatio?: number;
  /** Step-relative stop distance from the nearest filled rung (0 = legacy boundary). */
  readonly stopRatio?: number;
  /** Maximum bars a filled rung may remain open (0 = disabled). */
  readonly maxHoldBars?: number;
  /** Avoid assuming that an OHLC candle hits a newly entered rung's target. */
  readonly conservativeIntrabar?: boolean;
  /** Fraction of equity allocated across all rungs (default 1.0 = 100%). */
  readonly positionFraction?: number;
  /**
   * Per-side TAKER fee (percent) for stop / liquidation exits; entry and
   * take-profit use the maker fee (feePct / 2). Default: symmetric round-trip.
   */
  readonly takerExitFeePct?: number;
  /** Chop gate: when > 0, new ladder seeds are skipped while ADX(14) >= threshold. */
  readonly chopGateAdxThreshold?: number;
  /**
   * Account realized-drawdown kill, in percent below peak (0/undefined =
   * disabled, >= 100 = disabled). While flat and in breach, the engine
   * re-anchors peak to capital and pauses (paused-then-retry) instead of
   * dying permanently; live (advanceLadderBar) additionally flattens open
   * rungs at the breach bar (known residual divergence).
   */
  readonly maxDrawdownPct?: number;
  /** Candle timeframe the ADX gate is computed on (default "15m"). */
  readonly timeframe?: string;
  /** Funding cost per 8h held on open notional, in percent (longs pay positive rates). */
  readonly fundingRatePct8h?: number;
  /**
   * Maintenance-margin rate (fraction of notional) in the liquidation model.
   * Default 0 keeps the legacy 1/L distance.
   */
  readonly maintenanceMarginRate?: number;
  /** Base probability (0..1) that a touched entry level fills (default 1.0). */
  readonly makerFillProb?: number;
  /** Model adverse selection on maker fills when true (default false). */
  readonly adverseSelection?: boolean;
  /** Deterministic seed for the fill-probability RNG (default 12345). */
  readonly fillSeed?: number;
}

export interface LadderTrade {
  readonly side: "long" | "short";
  readonly rungIndex?: number;
  readonly entryBar: number;
  readonly exitBar: number;
  readonly entryPrice: number;
  readonly exitPrice: number;
  /** Net leveraged return fraction including round-trip fees. */
  readonly pnlPct: number;
  /** Absolute quote-currency PnL of the trade. */
  readonly pnlQuote?: number;
  readonly win: boolean;
  readonly isLiquidation: boolean;
  readonly exitReason?:
    | "target"
    | "stop"
    | "liquidation"
    | "max_hold"
    | "mark_to_market";
}

/** Funding interval in ms (perpetual funding settles every 8h). */
const FUNDING_INTERVAL_MS = 8 * 3_600_000;

export interface LadderResult {
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly winRate: number;
  readonly totalTrades: number;
  readonly profitFactor: number;
  readonly trades: readonly LadderTrade[];
}

export interface LadderSearchSpace {
  readonly rungs: readonly number[];
  readonly gridStepPct: readonly number[];
  readonly gridMaxGrids: readonly number[];
  readonly gridPauseAfterLossBars: readonly number[];
  /**
   * Optional R:R dial (take-profit = step × targetRatio). Absent = [1] —
   * the legacy inverted-R:R behavior. The universe funnel passes
   * GATE_TARGETS so walk-forward selection matches the geometry the gate
   * stage (and the live book) actually trade.
   */
  readonly targetRatio?: readonly number[];
  /**
   * Optional chop-gate ADX floor. Absent = [0] (gate disabled). The funnel
   * passes GATE_ADX_GATES; without it wf selection favors churn configs that
   * bleed OOS even when the gated combo is profitable.
   */
  readonly chopGateAdxThreshold?: readonly number[];
}

export interface LadderWalkForwardWindow {
  readonly trainStartIndex: number;
  readonly trainEndIndex: number;
  readonly testStartIndex: number;
  readonly testEndIndex: number;
  readonly params: LadderOptions;
  readonly testReturnPct: number;
  readonly testMaxDrawdownPct: number;
  readonly testTrades: number;
  readonly avgWinPct?: number;
  readonly avgLossPct?: number;
}

export interface LadderWalkForwardResult {
  readonly windows: readonly LadderWalkForwardWindow[];
  readonly aggregateReturnPct: number;
  readonly profitableWindowsPct: number;
  readonly maxDrawdownPct: number;
  readonly totalTrades: number;
  readonly avgWinPct?: number;
  readonly avgLossPct?: number;
}

interface Rung {
  rungIndex: number;
  side: "long" | "short";
  level: number; // entry level (pre-slippage)
  step: number;
  filled: boolean;
  entryPrice: number;
  entryBar: number;
}

export function liquidationPrice(
  side: "long" | "short",
  entryPrice: number,
  leverage: number,
  mmRate = 0,
): number {
  const l = Math.max(1, leverage);
  if (l <= 1) return 0;
  // Adverse move to liquidation = initial leverage distance minus the
  // maintenance-margin buffer (floored at 1% so a huge mmRate can never
  // produce a non-liquidating or inverted price).
  const move = Math.max(0.01, 1 / l - mmRate);
  return side === "long" ? entryPrice * (1 - move) : entryPrice * (1 + move);
}

interface LadderBacktestConfig {
  readonly leverage: number;
  readonly positionFraction: number;
  readonly targetRatio: number;
  readonly stopRatio: number;
  readonly maxHoldBars: number;
  readonly conservativeIntrabar: boolean;
  readonly makerFeePerSide: number;
  readonly takerFeePerSide: number;
  readonly targetFee: number;
  readonly stopFee: number;
  readonly onlyWithTrend: boolean;
  readonly rungs: number;
  readonly sizePerRung: number;
  readonly chopGateAdxThreshold: number;
  readonly makerFillProb: number;
  readonly adverseSelection: boolean;
  readonly trendFilterPeriod: number;
}

function normalizeLadderBacktestConfig(
  opts: LadderOptions,
): LadderBacktestConfig {
  const leverage = Math.max(1, opts.leverage ?? 1);
  const positionFraction = Math.max(0, Math.min(1, opts.positionFraction ?? 1));
  const rungs = Math.max(1, Math.floor(opts.rungs ?? 1));
  const makerFeePerSide = (opts.feePct ?? 0) / 100;
  const takerFeePerSide = (opts.takerExitFeePct ?? opts.feePct ?? 0) / 100;
  return {
    leverage,
    positionFraction,
    targetRatio: Math.max(0.001, opts.targetRatio ?? 1),
    stopRatio: Math.max(0, opts.stopRatio ?? 0),
    maxHoldBars: Math.max(0, Math.floor(opts.maxHoldBars ?? 0)),
    conservativeIntrabar: opts.conservativeIntrabar ?? true,
    makerFeePerSide,
    takerFeePerSide,
    targetFee: makerFeePerSide * 2,
    stopFee: makerFeePerSide + takerFeePerSide,
    onlyWithTrend: opts.onlyWithTrend ?? false,
    rungs,
    sizePerRung: positionFraction / rungs,
    chopGateAdxThreshold: Math.max(0, opts.chopGateAdxThreshold ?? 0),
    makerFillProb: Math.max(0, Math.min(1, opts.makerFillProb ?? 1)),
    adverseSelection: opts.adverseSelection ?? false,
    trendFilterPeriod: Math.max(0, opts.trendFilterPeriod ?? 0),
  };
}

function buildLadderTrendSeries(
  candles: readonly CandleLike[],
  opts: LadderOptions,
  period: number,
): readonly number[] | null {
  if (period <= 0) return null;
  const closes = candles.map((candle) => candle.close);
  return opts.trendFilterType === "ema"
    ? calculateEMA(closes, period)
    : calculateSMA(closes, period);
}

function emptyLadderResult(): LadderResult {
  return {
    totalReturnPct: 0,
    maxDrawdownPct: 0,
    winRate: 0,
    totalTrades: 0,
    profitFactor: 0,
    trades: [],
  };
}

function finalizeLadderResult(
  opts: LadderOptions,
  capital: number,
  maxDrawdown: number,
  totalWins: number,
  totalLosses: number,
  grossProfit: number,
  grossLoss: number,
  trades: readonly LadderTrade[],
): LadderResult {
  const totalTrades = totalWins + totalLosses;
  const totalReturnPct =
    opts.initialCapital > 0
      ? ((capital - opts.initialCapital) / opts.initialCapital) * 100
      : 0;
  const profitFactor =
    grossLoss > 0 ? grossProfit / grossLoss : grossProfit > 0 ? Infinity : 0;
  return {
    totalReturnPct,
    maxDrawdownPct: maxDrawdown * 100,
    winRate: totalTrades > 0 ? (totalWins / totalTrades) * 100 : 0,
    totalTrades,
    profitFactor,
    trades,
  };
}

export function runLadderGridBacktest(
  candles: readonly CandleLike[],
  opts: LadderOptions,
): LadderResult {
  if (!candles || candles.length === 0) {
    return emptyLadderResult();
  }

  let capital = opts.initialCapital;
  let peak = capital;
  // statPeak is never re-anchored: maxDrawdown must report the true
  // peak-to-trough over the whole run, even across kill re-anchors.
  let statPeak = capital;
  let maxDrawdown = 0;
  let totalWins = 0;
  let totalLosses = 0;
  let grossProfit = 0;
  let grossLoss = 0;
  let paused = 0;
  const trades: LadderTrade[] = [];
  const config = normalizeLadderBacktestConfig(opts);
  const {
    leverage,
    positionFraction,
    targetRatio,
    stopRatio,
    maxHoldBars,
    conservativeIntrabar,
    targetFee,
    stopFee,
    onlyWithTrend,
    rungs: N,
    sizePerRung,
    chopGateAdxThreshold,
    makerFillProb,
    adverseSelection,
    trendFilterPeriod,
  } = config;

  let fillSeed = opts.fillSeed ?? 12345;
  const fillRng = (): number => {
    fillSeed |= 0;
    fillSeed = (fillSeed + 0x6d2b79f5) | 0;
    let t = Math.imul(fillSeed ^ (fillSeed >>> 15), fillSeed | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };

  const fillsAtLevel = (
    candle: CandleLike,
    level: number,
    side: "long" | "short",
  ): boolean => {
    if (makerFillProb >= 1 && !adverseSelection) return true;
    const adverse =
      side === "long" ? candle.close < level : candle.close > level;
    const prob = adverseSelection && adverse ? 1 : makerFillProb;
    return fillRng() < prob;
  };

  const statsProvider =
    chopGateAdxThreshold > 0
      ? cachedCausalStats(candles, opts.timeframe ?? "15m")
      : null;

  // Pre-calculate trend filter series if enabled
  const trendSeries = buildLadderTrendSeries(candles, opts, trendFilterPeriod);

  let longRungs: Rung[] = [];
  let shortRungs: Rung[] = [];
  let longBase = 0;
  let shortBase = 0;

  const fundingForRung = (r: Rung, bar: number): number => {
    const rate = opts.fundingRatePct8h ?? 0;
    if (rate === 0 || r.entryBar === undefined) return 0;
    const entryMs = candles[r.entryBar]?.timestamp?.getTime() ?? NaN;
    const exitMs = candles[bar]?.timestamp?.getTime() ?? NaN;
    if (!Number.isFinite(entryMs) || !Number.isFinite(exitMs)) return 0;
    const intervals = Math.floor((exitMs - entryMs) / FUNDING_INTERVAL_MS);
    if (intervals <= 0) return 0;
    return (
      capital *
      positionFraction *
      leverage *
      ((rate / 100) * intervals) *
      (r.side === "long" ? -1 : 1)
    );
  };
  const closeRung = (
    r: Rung,
    exitPrice: number,
    reason: "target" | "stop" | "liquidation" | "max_hold" | "mark_to_market",
    bar: number,
  ): void => {
    const isLiquidation = reason === "liquidation";
    const fee = reason === "target" ? targetFee : stopFee;
    const pricePnl =
      r.side === "long"
        ? (exitPrice - r.entryPrice) / r.entryPrice
        : (r.entryPrice - exitPrice) / r.entryPrice;
    const net = pricePnl - fee;
    const leveragedReturn = isLiquidation ? -1 : net * leverage;
    const equityReturn = isLiquidation
      ? -sizePerRung
      : sizePerRung * leveragedReturn;
    const capitalBefore = capital;
    const funding = fundingForRung(r, bar);
    capital = Math.max(0, capitalBefore * (1 + equityReturn) + funding);
    peak = Math.max(peak, capital);
    statPeak = Math.max(statPeak, capital);
    const statDd = statPeak > 0 ? (statPeak - capital) / statPeak : 0;
    if (statDd > maxDrawdown) maxDrawdown = statDd;
    const win = !isLiquidation && net >= 0;
    if (isLiquidation || net < 0) {
      totalLosses++;
      grossLoss += Math.abs(leveragedReturn);
    } else {
      totalWins++;
      grossProfit += leveragedReturn;
    }
    trades.push({
      side: r.side,
      rungIndex: r.rungIndex,
      entryBar: r.entryBar,
      exitBar: bar,
      entryPrice: r.entryPrice,
      exitPrice,
      pnlPct: leveragedReturn,
      pnlQuote: capitalBefore * equityReturn,
      win,
      isLiquidation,
      exitReason: reason,
    });
  };

  const closeAll = (
    rungs: Rung[],
    exitPrice: number,
    reason: "target" | "stop" | "liquidation" | "max_hold" | "mark_to_market",
    bar: number,
  ): boolean => {
    let anyLoss = false;
    for (const r of rungs) {
      if (!r.filled) continue;
      const prev = capital;
      closeRung(r, exitPrice, reason, bar);
      if (capital < prev) anyLoss = true;
    }
    return anyLoss;
  };

  interface LadderSideState {
    rungs: Rung[];
    base: number;
  }

  interface LadderSideResult extends LadderSideState {
    paused: number;
  }

  const emptySide = (): LadderSideState => ({ rungs: [], base: 0 });

  const buildSideRungs = (
    side: "long" | "short",
    base: number,
    step: number,
  ): Rung[] => {
    const rungs: Rung[] = [];
    for (let k = 1; k <= N; k++) {
      rungs.push({
        rungIndex: k,
        side,
        level: side === "long" ? base - k * step : base + k * step,
        step,
        filled: false,
        entryPrice: 0,
        entryBar: 0,
      });
    }
    return rungs;
  };

  const seedSide = (
    side: "long" | "short",
    state: LadderSideState,
    candle: CandleLike,
    mid: number,
    step: number,
    trend: number | null,
    chopGateActive: boolean,
    ddBlocked: boolean,
  ): LadderSideState => {
    if (state.rungs.some((r) => r.filled)) return state;
    const trendPass =
      trend !== null &&
      !isNaN(trend) &&
      (side === "long" ? candle.close > trend : candle.close < trend);
    const canSeed =
      !chopGateActive && !ddBlocked && (!onlyWithTrend || trendPass);
    if (!canSeed) return emptySide();
    return { rungs: buildSideRungs(side, mid, step), base: mid };
  };

  const fillSideRungs = (
    side: "long" | "short",
    state: LadderSideState,
    candle: CandleLike,
    bar: number,
    slippage: number,
  ): void => {
    for (let k = 0; k < state.rungs.length; k++) {
      const rung = state.rungs[k];
      if (rung.filled) continue;
      const previousFilled = k === 0 || state.rungs[k - 1].filled;
      const touched =
        side === "long" ? candle.low <= rung.level : candle.high >= rung.level;
      if (
        !previousFilled ||
        !touched ||
        !fillsAtLevel(candle, rung.level, side)
      ) {
        continue;
      }
      rung.filled = true;
      rung.entryPrice =
        side === "long" ? rung.level * slippage : rung.level / slippage;
      rung.entryBar = bar;
    }
  };

  const pauseAfterLoss = (previousPaused: number, loss: boolean): number =>
    loss && opts.gridPauseAfterLossBars > 0
      ? opts.gridPauseAfterLossBars
      : previousPaused;

  const settleSideExit = (
    filled: Rung[],
    exitPrice: number,
    reason: "stop" | "liquidation",
    bar: number,
    previousPaused: number,
  ): LadderSideResult => {
    const loss = closeAll(filled, exitPrice, reason, bar);
    return {
      ...emptySide(),
      paused: pauseAfterLoss(previousPaused, loss),
    };
  };

  const settleBoundaryExit = (
    side: "long" | "short",
    state: LadderSideState,
    filled: Rung[],
    candle: CandleLike,
    step: number,
    slippage: number,
    bar: number,
    previousPaused: number,
  ): LadderSideResult | null => {
    const boundary =
      stopRatio > 0
        ? side === "long"
          ? Math.min(...filled.map((r) => r.entryPrice)) - step * stopRatio
          : Math.max(...filled.map((r) => r.entryPrice)) + step * stopRatio
        : side === "long"
          ? state.base - step * (N + opts.gridMaxGrids)
          : state.base + step * (N + opts.gridMaxGrids);
    const liquidationLevels = filled
      .map((r) => liquidationPrice(side, r.entryPrice, leverage))
      .filter((price) => price > 0);
    const liquidation =
      liquidationLevels.length > 0
        ? side === "long"
          ? Math.max(...liquidationLevels)
          : Math.min(...liquidationLevels)
        : 0;
    const liquidationTouched =
      liquidation > 0 &&
      (side === "long"
        ? candle.low <= liquidation
        : candle.high >= liquidation);
    if (liquidationTouched) {
      const exitPrice =
        liquidation *
        (side === "long"
          ? 1 - opts.slippageBps / 10000
          : 1 + opts.slippageBps / 10000);
      return settleSideExit(
        filled,
        exitPrice,
        "liquidation",
        bar,
        previousPaused,
      );
    }
    const boundaryTouched =
      side === "long" ? candle.low <= boundary : candle.high >= boundary;
    if (!boundaryTouched) return null;
    const exitPrice =
      boundary *
      (side === "long"
        ? 1 - opts.slippageBps / 10000
        : 1 + opts.slippageBps / 10000);
    return settleSideExit(filled, exitPrice, "stop", bar, previousPaused);
  };

  const settleTargets = (
    side: "long" | "short",
    state: LadderSideState,
    candle: CandleLike,
    slippage: number,
    bar: number,
    previousPaused: number,
  ): LadderSideResult => {
    const stillOpen: Rung[] = [];
    let anyFillClosed = false;
    for (const rung of state.rungs) {
      if (!rung.filled) {
        stillOpen.push(rung);
        continue;
      }
      const target =
        side === "long"
          ? rung.entryPrice + rung.step * targetRatio
          : rung.entryPrice - rung.step * targetRatio;
      const targetReached =
        (side === "long" ? candle.high >= target : candle.low <= target) &&
        (!conservativeIntrabar || rung.entryBar < bar);
      const maxHoldReached =
        maxHoldBars > 0 &&
        bar - rung.entryBar >= maxHoldBars &&
        rung.entryBar < bar;
      if (targetReached) {
        closeRung(
          rung,
          side === "long" ? target / slippage : target * slippage,
          "target",
          bar,
        );
        anyFillClosed = true;
      } else if (maxHoldReached) {
        closeRung(
          rung,
          candle.close *
            (side === "long"
              ? 1 - opts.slippageBps / 10000
              : 1 + opts.slippageBps / 10000),
          "max_hold",
          bar,
        );
        anyFillClosed = true;
      } else {
        stillOpen.push(rung);
      }
    }
    const openFilled = stillOpen.filter((r) => r.filled).length;
    const nextState =
      openFilled === 0 && anyFillClosed
        ? emptySide()
        : { rungs: stillOpen, base: state.base };
    return { ...nextState, paused: previousPaused };
  };

  const manageSide = (
    side: "long" | "short",
    state: LadderSideState,
    candle: CandleLike,
    step: number,
    slippage: number,
    bar: number,
    previousPaused: number,
  ): LadderSideResult => {
    if (state.rungs.length === 0) {
      return { ...state, paused: previousPaused };
    }
    fillSideRungs(side, state, candle, bar, slippage);
    const filled = state.rungs.filter((r) => r.filled);
    if (filled.length === 0) return { ...state, paused: previousPaused };
    const boundaryExit = settleBoundaryExit(
      side,
      state,
      filled,
      candle,
      step,
      slippage,
      bar,
      previousPaused,
    );
    if (boundaryExit) return boundaryExit;
    return settleTargets(side, state, candle, slippage, bar, previousPaused);
  };

  const startIndex = Math.max(trendFilterPeriod, 1);
  for (let i = startIndex; i < candles.length; i++) {
    const c = candles[i];
    const trend =
      trendSeries !== null && i < trendSeries.length ? trendSeries[i] : null;
    if (trendFilterPeriod > 0 && (trend === null || isNaN(trend))) continue;

    capital = Math.max(0, capital);
    peak = Math.max(peak, capital);
    statPeak = Math.max(statPeak, capital);
    let dd = peak > 0 ? (peak - capital) / peak : 0;
    const statDdTop = statPeak > 0 ? (statPeak - capital) / statPeak : 0;
    if (statDdTop > maxDrawdown) maxDrawdown = statDdTop;

    // Drawdown seed block + peak re-anchor, mirroring advanceLadderBar
    // (ladder-engine.ts). The backtest previously recorded maxDrawdown but
    // never enforced it, so sweeps fitted configs on recovery trades the
    // live engine refuses to take. While flat and in breach: re-anchor peak
    // to capital and pause, so both engines agree paused-then-retry.
    // NOTE: live also flattens open rungs at the breach bar; the backtest
    // lets them ride to their own exits (known residual divergence).
    // Helper keeps runLadderGridBacktest under the complexity gate.
    const drawdownSeedBlock = (): boolean => {
      const ddCap = opts.maxDrawdownPct ?? 0;
      if (!(ddCap > 0 && ddCap < 100 && peak > 0 && dd >= ddCap / 100))
        return false;
      if (
        !longRungs.some((r) => r.filled) &&
        !shortRungs.some((r) => r.filled)
      ) {
        peak = capital;
        dd = 0;
        paused = Math.max(
          paused,
          Math.max(0, Math.floor(opts.gridPauseAfterLossBars ?? 24)),
        );
        return false;
      }
      return true;
    };
    const ddBlocked = drawdownSeedBlock();

    if (paused > 0) {
      paused--;
      continue;
    }

    const mid = c.open;
    const step = mid * (opts.gridStepPct / 100);
    const slippage = 1 + opts.slippageBps / 10000;

    const chopGateActive =
      statsProvider !== null && statsProvider(i).adx14 >= chopGateAdxThreshold;

    const seededLong = seedSide(
      "long",
      { rungs: longRungs, base: longBase },
      c,
      mid,
      step,
      trend,
      chopGateActive,
      ddBlocked,
    );
    longRungs = seededLong.rungs;
    longBase = seededLong.base;

    const seededShort = seedSide(
      "short",
      { rungs: shortRungs, base: shortBase },
      c,
      mid,
      step,
      trend,
      chopGateActive,
      ddBlocked,
    );
    shortRungs = seededShort.rungs;
    shortBase = seededShort.base;

    const managedLong = manageSide(
      "long",
      { rungs: longRungs, base: longBase },
      c,
      step,
      slippage,
      i,
      paused,
    );
    longRungs = managedLong.rungs;
    longBase = managedLong.base;
    paused = managedLong.paused;

    const managedShort = manageSide(
      "short",
      { rungs: shortRungs, base: shortBase },
      c,
      step,
      slippage,
      i,
      paused,
    );
    shortRungs = managedShort.rungs;
    shortBase = managedShort.base;
    paused = managedShort.paused;
  }
  // Mark any still-open filled rungs to market at the final close.
  const lastBarIndex = candles.length - 1;
  const lastClose = candles[lastBarIndex].close;
  if (longRungs.some((r) => r.filled)) {
    closeAll(
      longRungs.filter((r) => r.filled),
      lastClose,
      "mark_to_market",
      lastBarIndex,
    );
  }
  if (shortRungs.some((r) => r.filled)) {
    closeAll(
      shortRungs.filter((r) => r.filled),
      lastClose,
      "mark_to_market",
      lastBarIndex,
    );
  }

  return finalizeLadderResult(
    opts,
    capital,
    maxDrawdown,
    totalWins,
    totalLosses,
    grossProfit,
    grossLoss,
    trades,
  );
}

export function findBestLadderGridParams(
  trainCandles: readonly CandleLike[],
  searchSpace: LadderSearchSpace,
  baseOptions: Omit<
    LadderOptions,
    "rungs" | "gridStepPct" | "gridMaxGrids" | "gridPauseAfterLossBars"
  >,
  candidateFilter?: LadderCandidateFilter,
): LadderOptions & { result: LadderResult } {
  let bestOverall: (LadderOptions & { result: LadderResult }) | null = null;
  let bestEligible: (LadderOptions & { result: LadderResult }) | null = null;

  // When stopRatio is active, the stop boundary is derived from the deepest
  // filled rung and stopRatio; gridMaxGrids is not read by the backtest. Do
  // not spend four full simulations on equivalent candidates during every
  // walk-forward window. Keep the first value for deterministic output and
  // preserve the full sweep when the legacy grid-distance stop is active.
  const gridMaxGrids =
    (baseOptions.stopRatio ?? 0) > 0
      ? [searchSpace.gridMaxGrids[0] ?? 1]
      : searchSpace.gridMaxGrids;
  const targetRatios = searchSpace.targetRatio ?? [1];
  const chopGates = searchSpace.chopGateAdxThreshold ?? [0];
  for (const rungs of searchSpace.rungs) {
    for (const gridStepPct of searchSpace.gridStepPct) {
      for (const maxGrids of gridMaxGrids) {
        for (const gridPauseAfterLossBars of searchSpace.gridPauseAfterLossBars) {
          for (const targetRatio of targetRatios) {
            for (const chopGateAdxThreshold of chopGates) {
              const options: LadderOptions = {
                ...baseOptions,
                rungs,
                gridStepPct,
                gridMaxGrids: maxGrids,
                gridPauseAfterLossBars,
                targetRatio,
                chopGateAdxThreshold,
              };
              const result = runLadderGridBacktest(trainCandles, options);
              const candidate = { ...options, result };
              if (
                !bestOverall ||
                result.totalReturnPct > bestOverall.result.totalReturnPct
              ) {
                bestOverall = candidate;
              }
              if (
                candidateFilter?.(trainCandles, options, result) !== false &&
                (!bestEligible ||
                  result.totalReturnPct > bestEligible.result.totalReturnPct)
              ) {
                bestEligible = candidate;
              }
            }
          }
        }
      }
    }
  }

  const best = bestEligible ?? bestOverall;
  if (!best) {
    throw new Error(
      "Ladder grid search space produced no parameter combinations.",
    );
  }

  return best;
}

/**
 * Optional training-window constraint used by universe selection. The
 * selector still falls back to the unconstrained winner when no combination
 * meets the constraint, allowing the downstream gate to reject it explicitly
 * instead of silently producing an empty walk-forward result.
 */
export type LadderCandidateFilter = (
  trainCandles: readonly CandleLike[],
  candidate: LadderOptions,
  result: LadderResult,
) => boolean;

export function runLadderGridWalkForward(
  candles: readonly CandleLike[],
  options: {
    readonly trainWindow: number;
    readonly testWindow: number;
    readonly initialCapital: number;
    readonly searchSpace: LadderSearchSpace;
    readonly baseOptions: Omit<
      LadderOptions,
      | "rungs"
      | "gridStepPct"
      | "gridMaxGrids"
      | "gridPauseAfterLossBars"
      | "initialCapital"
    >;
    readonly candidateFilter?: LadderCandidateFilter;
  },
): LadderWalkForwardResult {
  const {
    trainWindow,
    testWindow,
    initialCapital,
    searchSpace,
    baseOptions,
    candidateFilter,
  } = options;

  const windows: LadderWalkForwardWindow[] = [];
  let runningCapital = initialCapital;
  let aggregateMaxDrawdown = 0;
  let totalTrades = 0;
  let totalWinPct = 0;
  let totalLossPct = 0;
  let totalWins = 0;
  let totalLosses = 0;

  for (
    let start = 0;
    start + trainWindow + testWindow <= candles.length;
    start += testWindow
  ) {
    const trainCandles = candles.slice(start, start + trainWindow);
    const testCandles = candles.slice(
      start + trainWindow,
      start + trainWindow + testWindow,
    );

    const best = findBestLadderGridParams(
      trainCandles,
      searchSpace,
      {
        ...baseOptions,
        initialCapital: runningCapital,
      },
      candidateFilter,
    );

    const testResult = runLadderGridBacktest(testCandles, {
      ...best,
      initialCapital: runningCapital,
    });

    const boundedWindowReturn = Math.max(
      -100,
      Math.min(100, testResult.totalReturnPct),
    );
    runningCapital *= 1 + boundedWindowReturn / 100;
    aggregateMaxDrawdown = Math.max(
      aggregateMaxDrawdown,
      testResult.maxDrawdownPct,
    );
    totalTrades += testResult.totalTrades;

    let windowWins = 0;
    let windowLosses = 0;
    let windowWinPct = 0;
    let windowLossPct = 0;
    for (const trade of testResult.trades) {
      if (trade.pnlPct >= 0) {
        windowWins += 1;
        windowWinPct += trade.pnlPct;
      } else {
        windowLosses += 1;
        windowLossPct += Math.abs(trade.pnlPct);
      }
    }
    totalWins += windowWins;
    totalLosses += windowLosses;
    totalWinPct += windowWinPct;
    totalLossPct += windowLossPct;

    windows.push({
      trainStartIndex: start,
      trainEndIndex: start + trainWindow,
      testStartIndex: start + trainWindow,
      testEndIndex: start + trainWindow + testWindow,
      params: best,
      testReturnPct: testResult.totalReturnPct,
      testMaxDrawdownPct: testResult.maxDrawdownPct,
      testTrades: testResult.totalTrades,
      avgWinPct: windowWins > 0 ? (windowWinPct / windowWins) * 100 : undefined,
      avgLossPct:
        windowLosses > 0 ? (windowLossPct / windowLosses) * 100 : undefined,
    });
  }

  const profitableCount = windows.filter((w) => w.testReturnPct > 0).length;
  const profitableWindowsPct =
    windows.length > 0 ? (profitableCount / windows.length) * 100 : 0;
  const aggregateReturnPct =
    initialCapital > 0
      ? ((runningCapital - initialCapital) / initialCapital) * 100
      : 0;

  return {
    windows,
    aggregateReturnPct,
    profitableWindowsPct,
    maxDrawdownPct: aggregateMaxDrawdown,
    totalTrades,
    avgWinPct: totalWins > 0 ? (totalWinPct / totalWins) * 100 : undefined,
    avgLossPct:
      totalLosses > 0 ? (totalLossPct / totalLosses) * 100 : undefined,
  };
}
