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

export function runLadderGridBacktest(
  candles: readonly CandleLike[],
  opts: LadderOptions,
): LadderResult {
  if (!candles || candles.length === 0) {
    return {
      totalReturnPct: 0,
      maxDrawdownPct: 0,
      winRate: 0,
      totalTrades: 0,
      profitFactor: 0,
      trades: [],
    };
  }

  let capital = opts.initialCapital;
  let peak = capital;
  let maxDrawdown = 0;
  let totalWins = 0;
  let totalLosses = 0;
  let grossProfit = 0;
  let grossLoss = 0;
  let paused = 0;
  const trades: LadderTrade[] = [];
  const leverage = Math.max(1, opts.leverage ?? 1);
  const positionFraction = Math.max(0, Math.min(1, opts.positionFraction ?? 1));
  const targetRatio = Math.max(0.001, opts.targetRatio ?? 1);
  const stopRatio = Math.max(0, opts.stopRatio ?? 0);
  const maxHoldBars = Math.max(0, Math.floor(opts.maxHoldBars ?? 0));
  const conservativeIntrabar = opts.conservativeIntrabar ?? true;
  const makerFeePerSide = (opts.feePct ?? 0) / 100;
  const takerFeePerSide = (opts.takerExitFeePct ?? opts.feePct ?? 0) / 100;
  const targetFee = makerFeePerSide * 2;
  const stopFee = makerFeePerSide + takerFeePerSide;
  const onlyWithTrend = opts.onlyWithTrend ?? false;
  const N = Math.max(1, Math.floor(opts.rungs ?? 1));
  const sizePerRung = positionFraction / N;
  const chopGateAdxThreshold = Math.max(0, opts.chopGateAdxThreshold ?? 0);
  const makerFillProb = Math.max(0, Math.min(1, opts.makerFillProb ?? 1));
  const adverseSelection = opts.adverseSelection ?? false;

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
      ? makeCausalSymbolStats(candles, opts.timeframe ?? "15m")
      : null;

  // Pre-calculate trend filter series if enabled
  const trendFilterPeriod = Math.max(0, opts.trendFilterPeriod ?? 0);
  const trendSeries =
    trendFilterPeriod > 0
      ? opts.trendFilterType === "ema"
        ? calculateEMA(
            candles.map((c) => c.close),
            trendFilterPeriod,
          )
        : calculateSMA(
            candles.map((c) => c.close),
            trendFilterPeriod,
          )
      : null;

  let longRungs: Rung[] = [];
  let shortRungs: Rung[] = [];
  let longBase = 0;
  let shortBase = 0;

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
    // Funding accrued while the rung was open (whole 8h intervals, charged at
    // close; longs pay a positive rate, shorts receive it).
    let funding = 0;
    if ((opts.fundingRatePct8h ?? 0) !== 0 && r.entryBar !== undefined) {
      const entryMs = candles[r.entryBar]?.timestamp?.getTime() ?? NaN;
      const exitMs = candles[bar]?.timestamp?.getTime() ?? NaN;
      if (Number.isFinite(entryMs) && Number.isFinite(exitMs)) {
        const intervals = Math.floor((exitMs - entryMs) / FUNDING_INTERVAL_MS);
        if (intervals > 0) {
          funding =
            capitalBefore *
            positionFraction *
            leverage *
            ((opts.fundingRatePct8h! / 100) * intervals) *
            (r.side === "long" ? -1 : 1);
        }
      }
    }
    capital = Math.max(0, capitalBefore * (1 + equityReturn) + funding);
    peak = Math.max(peak, capital);
    const dd = peak > 0 ? (peak - capital) / peak : 0;
    if (dd > maxDrawdown) maxDrawdown = dd;
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

  const startIndex = Math.max(trendFilterPeriod, 1);
  for (let i = startIndex; i < candles.length; i++) {
    const c = candles[i];
    const trend =
      trendSeries !== null && i < trendSeries.length ? trendSeries[i] : null;
    if (trendFilterPeriod > 0 && (trend === null || isNaN(trend))) continue;

    capital = Math.max(0, capital);
    peak = Math.max(peak, capital);
    const dd = peak > 0 ? (peak - capital) / peak : 0;
    if (dd > maxDrawdown) maxDrawdown = dd;

    if (paused > 0) {
      paused--;
      continue;
    }

    const mid = c.open;
    const step = mid * (opts.gridStepPct / 100);
    const slippage = 1 + opts.slippageBps / 10000;

    const chopGateActive =
      statsProvider !== null && statsProvider(i).adx14 >= chopGateAdxThreshold;

    // --- (Re-)seed long ladder while no rung is filled ---
    // When flat, re-anchor to the current bar open each bar. Once a rung fills,
    // the anchor locks so the ladder can fill the remaining staggered levels.
    {
      const hasFilledLong = longRungs.some((r) => r.filled);
      if (!hasFilledLong) {
        const allowLong =
          !chopGateActive &&
          (!onlyWithTrend ||
            (trend !== null && !isNaN(trend) && c.close > trend));
        if (allowLong) {
          longBase = mid;
          longRungs = [];
          for (let k = 1; k <= N; k++) {
            longRungs.push({
              rungIndex: k,
              side: "long",
              level: mid - k * step,
              step,
              filled: false,
              entryPrice: 0,
              entryBar: 0,
            });
          }
        } else {
          longRungs = [];
          longBase = 0;
        }
      }
    }

    // --- (Re-)seed short ladder while no rung is filled ---
    {
      const hasFilledShort = shortRungs.some((r) => r.filled);
      if (!hasFilledShort) {
        const allowShort =
          !chopGateActive &&
          (!onlyWithTrend ||
            (trend !== null && !isNaN(trend) && c.close < trend));
        if (allowShort) {
          shortBase = mid;
          shortRungs = [];
          for (let k = 1; k <= N; k++) {
            shortRungs.push({
              rungIndex: k,
              side: "short",
              level: mid + k * step,
              step,
              filled: false,
              entryPrice: 0,
              entryBar: 0,
            });
          }
        } else {
          shortRungs = [];
          shortBase = 0;
        }
      }
    }

    // === Manage LONG ladder ===
    if (longRungs.length > 0) {
      // Fill rungs progressively (rung k fills once rung k-1 is filled).
      for (let k = 0; k < longRungs.length; k++) {
        const r = longRungs[k];
        if (r.filled) continue;
        const prevFilled = k === 0 || longRungs[k - 1].filled;
        if (
          prevFilled &&
          c.low <= r.level &&
          fillsAtLevel(c, r.level, "long")
        ) {
          r.filled = true;
          r.entryPrice = r.level * slippage;
          r.entryBar = i;
        }
      }
      const filledLong = longRungs.filter((r) => r.filled);
      if (filledLong.length > 0) {
        const boundary =
          stopRatio > 0
            ? Math.min(...filledLong.map((r) => r.entryPrice)) -
              step * stopRatio
            : longBase - step * (N + opts.gridMaxGrids);
        const longLiqs = filledLong
          .map((r) => liquidationPrice("long", r.entryPrice, leverage))
          .filter((p) => p > 0);
        const liq = longLiqs.length > 0 ? Math.max(...longLiqs) : 0;
        if (liq > 0 && c.low <= liq) {
          const loss = closeAll(
            filledLong,
            liq * (1 - opts.slippageBps / 10000),
            "liquidation",
            i,
          );
          longRungs = [];
          longBase = 0;
          if (loss && opts.gridPauseAfterLossBars > 0) {
            paused = opts.gridPauseAfterLossBars;
          }
        } else if (c.low <= boundary) {
          const loss = closeAll(
            filledLong,
            boundary * (1 - opts.slippageBps / 10000),
            "stop",
            i,
          );
          longRungs = [];
          longBase = 0;
          if (loss && opts.gridPauseAfterLossBars > 0) {
            paused = opts.gridPauseAfterLossBars;
          }
        } else {
          // Take-profits for each filled rung that reached its target.
          const stillOpen: Rung[] = [];
          let anyFillClosed = false;
          for (const r of longRungs) {
            if (!r.filled) {
              stillOpen.push(r);
              continue;
            }
            const target = r.entryPrice + r.step * targetRatio;
            const targetReached =
              c.high >= target && (!conservativeIntrabar || r.entryBar < i);
            if (targetReached) {
              closeRung(r, target / slippage, "target", i);
              anyFillClosed = true;
            } else if (
              maxHoldBars > 0 &&
              i - r.entryBar >= maxHoldBars &&
              r.entryBar < i
            ) {
              closeRung(
                r,
                c.close * (1 - opts.slippageBps / 10000),
                "max_hold",
                i,
              );
              anyFillClosed = true;
            } else {
              stillOpen.push(r);
            }
          }
          longRungs = stillOpen;
          // If all FILLED rungs closed but unfilled resting rungs remain,
          // cancel them and go flat (wait for the next oscillation).
          const openFilled = longRungs.filter((r) => r.filled).length;
          if (openFilled === 0 && anyFillClosed) {
            longRungs = [];
            longBase = 0;
          }
        }
      }
    }

    // === Manage SHORT ladder ===
    if (shortRungs.length > 0) {
      for (let k = 0; k < shortRungs.length; k++) {
        const r = shortRungs[k];
        if (r.filled) continue;
        const prevFilled = k === 0 || shortRungs[k - 1].filled;
        if (
          prevFilled &&
          c.high >= r.level &&
          fillsAtLevel(c, r.level, "short")
        ) {
          r.filled = true;
          r.entryPrice = r.level / slippage;
          r.entryBar = i;
        }
      }
      const filledShort = shortRungs.filter((r) => r.filled);
      if (filledShort.length > 0) {
        const boundary =
          stopRatio > 0
            ? Math.max(...filledShort.map((r) => r.entryPrice)) +
              step * stopRatio
            : shortBase + step * (N + opts.gridMaxGrids);
        const shortLiqs = filledShort
          .map((r) => liquidationPrice("short", r.entryPrice, leverage))
          .filter((p) => p > 0);
        const liq = shortLiqs.length > 0 ? Math.min(...shortLiqs) : 0;
        if (liq > 0 && c.high >= liq) {
          const loss = closeAll(
            filledShort,
            liq * (1 + opts.slippageBps / 10000),
            "liquidation",
            i,
          );
          shortRungs = [];
          shortBase = 0;
          if (loss && opts.gridPauseAfterLossBars > 0) {
            paused = opts.gridPauseAfterLossBars;
          }
        } else if (c.high >= boundary) {
          const loss = closeAll(
            filledShort,
            boundary * (1 + opts.slippageBps / 10000),
            "stop",
            i,
          );
          shortRungs = [];
          shortBase = 0;
          if (loss && opts.gridPauseAfterLossBars > 0) {
            paused = opts.gridPauseAfterLossBars;
          }
        } else {
          const stillOpen: Rung[] = [];
          let anyFillClosed = false;
          for (const r of shortRungs) {
            if (!r.filled) {
              stillOpen.push(r);
              continue;
            }
            const target = r.entryPrice - r.step * targetRatio;
            const targetReached =
              c.low <= target && (!conservativeIntrabar || r.entryBar < i);
            if (targetReached) {
              closeRung(r, target * slippage, "target", i);
              anyFillClosed = true;
            } else if (
              maxHoldBars > 0 &&
              i - r.entryBar >= maxHoldBars &&
              r.entryBar < i
            ) {
              closeRung(
                r,
                c.close * (1 + opts.slippageBps / 10000),
                "max_hold",
                i,
              );
              anyFillClosed = true;
            } else {
              stillOpen.push(r);
            }
          }
          shortRungs = stillOpen;
          const openFilled = shortRungs.filter((r) => r.filled).length;
          if (openFilled === 0 && anyFillClosed) {
            shortRungs = [];
            shortBase = 0;
          }
        }
      }
    }
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

  const totalTrades = totalWins + totalLosses;
  const totalReturnPct =
    opts.initialCapital > 0
      ? ((capital - opts.initialCapital) / opts.initialCapital) * 100
      : 0;
  const maxDrawdownPct = maxDrawdown * 100;
  const winRate = totalTrades > 0 ? (totalWins / totalTrades) * 100 : 0;
  const profitFactor =
    grossLoss > 0 ? grossProfit / grossLoss : grossProfit > 0 ? Infinity : 0;

  return {
    totalReturnPct,
    maxDrawdownPct,
    winRate,
    totalTrades,
    profitFactor,
    trades,
  };
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

  for (const rungs of searchSpace.rungs) {
    for (const gridStepPct of searchSpace.gridStepPct) {
      for (const maxGrids of gridMaxGrids) {
        for (const gridPauseAfterLossBars of searchSpace.gridPauseAfterLossBars) {
          const options: LadderOptions = {
            ...baseOptions,
            rungs,
            gridStepPct,
            gridMaxGrids: maxGrids,
            gridPauseAfterLossBars,
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
