/**
 * Market-neutral grid scalping engine.
 *
 * Ported from scripts/grid-research.ts.  Places symmetric buy/sell grids around
 * the current open price, captures oscillations, and pauses for a configurable
 * number of bars after a losing trade.  A simple SMA trend filter prevents
 * entries when the trend direction is unclear.
 */

import type { CandleLike } from "./types.js";
import { makeCausalSymbolStats } from "./symbol-stats.js";

/**
 * Walk-forward selection runs dozens of backtests over the same candle
 * array (one per search-space candidate). The ADX/ATR series is a pure
 * function of the candles, so share one provider per array instead of
 * rebuilding it per backtest (measured 30x: 1ms vs 33ms per 6k-bar
 * backtest with the chop gate on). WeakMap lets dead slices GC.
 */
type StatsProviderCache = WeakMap<readonly CandleLike[], CausalStatsProvider>;
const statsProviderCache: StatsProviderCache = new WeakMap();
function cachedStatsProvider(
  candles: readonly CandleLike[],
): CausalStatsProvider {
  const hit = statsProviderCache.get(candles);
  if (hit) return hit;
  const provider = makeCausalSymbolStats(candles, "15m");
  statsProviderCache.set(candles, provider);
  return provider;
}

export interface GridOptions {
  /** Grid step as a percentage of the mid price (e.g. 0.4 = 0.4%). */
  readonly gridStepPct: number;
  /** Maximum number of grid levels in the adverse direction before stopping out. */
  readonly gridMaxGrids: number;
  /** Bars to pause after a losing stop-out (0 = no pause). */
  readonly gridPauseAfterLossBars: number;
  /** Total fee percent for a round-trip (e.g. 0.04 for 0.02% maker each side). */
  readonly feePct: number;
  /** Slippage in basis points applied to limit fills. */
  readonly slippageBps: number;
  /** Initial capital in quote currency. */
  readonly initialCapital: number;
  /** SMA period used to confirm trend direction before entering. */
  readonly trendFilterPeriod: number;
  /** Leverage multiplier. 1 = spot-style (no liquidation). */
  readonly leverage: number;
  /** When true, only enter long above the trend SMA and short below it. */
  readonly onlyWithTrend?: boolean;
  /** Target distance as a multiple of the grid step (default 1.0). */
  readonly targetRatio?: number;
  /**
   * Chop gate: when > 0, NEW entries are skipped while the causal ADX(14)
   * is at or above this threshold (trending market). Open positions keep
   * their normal exit handling. 0/undefined disables the gate.
   */
  readonly chopGateAdxThreshold?: number;
  /**
   * Fraction of equity (0..1) allocated as notional to each grid position.
   * Default 1 = all-in. Below 1, the equity curve and drawdown scale down
   * while per-trade edge metrics (win rate / profit factor / expectancy from
   * pnlPct) are unchanged — used to tame drawdown (e.g. ETH 15m) and size
   * capital across positions.
   */
  readonly positionFraction?: number;
  /**
   * Base probability (0..1) that a touched entry level actually fills.
   * Default 1 = optimistic "touched = filled". Lower values model queue /
   * partial-fill risk on maker (limit) entries.
   */
  readonly makerFillProb?: number;
  /**
   * Model adverse selection when true: a touch whose bar CLOSES through the
   * entry level (price grinding through — the loss-prone case) fills with
   * probability 1, while a recovered wick (win-prone) fills with probability
   * makerFillProb. Default false (uniform fill probability).
   */
  readonly adverseSelection?: boolean;
  /**
   * Per-side TAKER fee (percent) for stop / liquidation exits; entry and
   * take-profit use the maker fee (feePct). Default: exit leg uses feePct too
   * (symmetric round-trip = current behavior).
   */
  readonly takerExitFeePct?: number;
  /** Deterministic seed for the fill-probability RNG (default 12345). */
  readonly fillSeed?: number;
  /**
   * Optional causal entry-direction overlay for research/backtest callers.
   * A value at bar i limits new entries on that bar to the named side;
   * `flat` blocks new entries. Undefined entries leave the grid unchanged.
   * This is deliberately an input array rather than a live model callback so
   * a walk-forward caller can prove that every decision was available before
   * the bar being traded.
   */
  readonly entryDirectionByBar?: readonly (
    | "long"
    | "short"
    | "flat"
    | undefined
  )[];
}

export interface GridSearchSpace {
  readonly gridStepPct: readonly number[];
  readonly gridMaxGrids: readonly number[];
  readonly gridPauseAfterLossBars: readonly number[];
}

export interface GridTrade {
  readonly side: "long" | "short";
  readonly entryBar: number;
  readonly exitBar: number;
  readonly entryPrice: number;
  readonly exitPrice: number;
  /** Net leveraged return fraction including round-trip fees (e.g. 0.01 = 1%). */
  readonly pnlPct: number;
  /** Absolute quote-currency PnL of the trade (negative for losses/liquidation). */
  readonly pnlQuote: number;
  readonly win: boolean;
  readonly isLiquidation: boolean;
}

export interface GridResult {
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly winRate: number;
  readonly totalTrades: number;
  readonly profitFactor: number;
  readonly trades: readonly GridTrade[];
}

export interface GridWalkForwardWindow {
  readonly trainStartIndex: number;
  readonly trainEndIndex: number;
  readonly testStartIndex: number;
  readonly testEndIndex: number;
  readonly params: GridOptions;
  readonly testReturnPct: number;
  readonly testMaxDrawdownPct: number;
  readonly testTrades: number;
  /**
   * Mean winning trade return (%) in this window's test slice
   * (pnlPct of winning trades × 100); undefined when the window had no
   * winning trades.
   */
  readonly avgWinPct?: number;
  /**
   * Mean losing trade magnitude (%) in this window's test slice
   * (|pnlPct| of losing trades × 100); undefined when the window had no
   * losing trades.
   */
  readonly avgLossPct?: number;
}

export interface GridWalkForwardResult {
  readonly windows: GridWalkForwardWindow[];
  readonly aggregateReturnPct: number;
  readonly profitableWindowsPct: number;
  readonly maxDrawdownPct: number;
  readonly totalTrades: number;
  /**
   * Trade-weighted mean winning trade return (%) across all window test
   * slices (Σ win pnlPct / wins × 100); undefined when no window had a
   * winning trade.
   */
  readonly avgWinPct?: number;
  /**
   * Trade-weighted mean losing trade magnitude (%) across all window test
   * slices (Σ |loss pnlPct| / losses × 100); undefined when no window had a
   * losing trade. Together with avgWinPct this gives the structural
   * win/loss asymmetry the funnel gate requires (breakevenWinRate =
   * avgLossPct / (avgWinPct + avgLossPct) ≤ 0.40 → target ≥ 1.5× stop).
   */
  readonly avgLossPct?: number;
}

function sma(
  candles: readonly CandleLike[],
  i: number,
  period: number,
): number | null {
  if (i < period) return null;
  let sum = 0;
  for (let j = i - period + 1; j <= i; j++) sum += candles[j].close;
  return sum / period;
}

function liquidationPrice(
  side: "long" | "short",
  entryPrice: number,
  leverage: number,
): number {
  const l = Math.max(1, leverage);
  if (l <= 1) return 0;
  return side === "long" ? entryPrice * (1 - 1 / l) : entryPrice * (1 + 1 / l);
}

/** Option values normalized once per run (defaults applied, ranges clamped). */
interface GridRuntime {
  readonly leverage: number;
  readonly chopGateAdxThreshold: number;
  readonly positionFraction: number;
  readonly makerFillProb: number;
  readonly adverseSelection: boolean;
  readonly onlyWithTrend: boolean;
  readonly targetRatio: number;
  readonly targetFee: number;
  readonly stopFee: number;
}

function gridRuntime(options: GridOptions): GridRuntime {
  const makerFeePerSide = options.feePct / 100;
  const takerFeePerSide = (options.takerExitFeePct ?? options.feePct) / 100;
  return {
    leverage: Math.max(1, options.leverage ?? 1),
    chopGateAdxThreshold: Math.max(0, options.chopGateAdxThreshold ?? 0),
    positionFraction: Math.max(0, Math.min(1, options.positionFraction ?? 1)),
    makerFillProb: Math.max(0, Math.min(1, options.makerFillProb ?? 1)),
    adverseSelection: options.adverseSelection ?? false,
    onlyWithTrend: options.onlyWithTrend ?? false,
    targetRatio: options.targetRatio ?? 1,
    targetFee: makerFeePerSide * 2,
    stopFee: makerFeePerSide + takerFeePerSide,
  };
}

/** A filled grid entry on one bar (positionSize: 1 = long, -1 = short). */
interface GridEntry {
  readonly positionSize: 1 | -1;
  readonly entryPrice: number;
  readonly entryBar: number;
}

type CausalStatsProvider = ReturnType<typeof makeCausalSymbolStats>;

function tryEnterGridPosition(
  barIndex: number,
  c: CandleLike,
  trend: number,
  mid: number,
  step: number,
  slippage: number,
  runtime: GridRuntime,
  statsProvider: CausalStatsProvider | null,
  entryDirectionByBar: GridOptions["entryDirectionByBar"],
  fillsAtLevel: (
    candle: CandleLike,
    level: number,
    side: "long" | "short",
  ) => boolean,
): GridEntry | null {
  const entryDirection = entryDirectionByBar?.[barIndex];
  if (entryDirection === "flat") return null;

  // Chop gate: trending markets are where grid inventory gets run over;
  // sit out until ADX says the market is ranging again.
  if (
    statsProvider !== null &&
    statsProvider(barIndex).adx14 >= runtime.chopGateAdxThreshold
  ) {
    return null;
  }
  const buyLevel = mid - step;
  const sellLevel = mid + step;
  const allowLong =
    (entryDirection === undefined || entryDirection === "long") &&
    (!runtime.onlyWithTrend || c.close > trend);
  const allowShort =
    (entryDirection === undefined || entryDirection === "short") &&
    (!runtime.onlyWithTrend || c.close < trend);
  if (allowLong && c.low <= buyLevel && fillsAtLevel(c, buyLevel, "long")) {
    return {
      positionSize: 1,
      entryPrice: buyLevel * slippage,
      entryBar: barIndex,
    };
  }
  if (
    allowShort &&
    c.high >= sellLevel &&
    fillsAtLevel(c, sellLevel, "short")
  ) {
    return {
      positionSize: -1,
      entryPrice: sellLevel / slippage,
      entryBar: barIndex,
    };
  }
  return null;
}

/** Exit decision for an open grid position on one bar (null = stay in). */
interface GridExit {
  readonly side: "long" | "short";
  readonly exitPrice: number;
  readonly exitReason: "target" | "stop" | "liquidation";
}

function longGridExitDecision(
  c: CandleLike,
  entryPrice: number,
  step: number,
  slippage: number,
  options: GridOptions,
  runtime: GridRuntime,
): GridExit | null {
  const target = entryPrice + step * runtime.targetRatio;
  const stop = entryPrice - step * options.gridMaxGrids;
  const liq = liquidationPrice("long", entryPrice, runtime.leverage);
  if (liq > 0 && c.low <= liq) {
    return {
      side: "long",
      exitPrice: liq * slippage,
      exitReason: "liquidation",
    };
  }
  if (c.high >= target) {
    return { side: "long", exitPrice: target / slippage, exitReason: "target" };
  }
  if (c.low <= stop) {
    return { side: "long", exitPrice: stop * slippage, exitReason: "stop" };
  }
  return null;
}

function shortGridExitDecision(
  c: CandleLike,
  entryPrice: number,
  step: number,
  slippage: number,
  options: GridOptions,
  runtime: GridRuntime,
): GridExit | null {
  const target = entryPrice - step * runtime.targetRatio;
  const stop = entryPrice + step * options.gridMaxGrids;
  const liq = liquidationPrice("short", entryPrice, runtime.leverage);
  if (liq > 0 && c.high >= liq) {
    return {
      side: "short",
      exitPrice: liq / slippage,
      exitReason: "liquidation",
    };
  }
  if (c.low <= target) {
    return {
      side: "short",
      exitPrice: target * slippage,
      exitReason: "target",
    };
  }
  if (c.high >= stop) {
    return { side: "short", exitPrice: stop / slippage, exitReason: "stop" };
  }
  return null;
}

/**
 * Money math shared by the in-run closeTrade path and the final
 * mark-to-market settle: price PnL → fees → leverage → positionFraction →
 * capital. Pure; the caller applies the returned values to its state.
 */
interface GridSettlement {
  readonly capital: number;
  readonly leveragedReturn: number;
  readonly pnlQuote: number;
  readonly win: boolean;
  /** true = liquidation or net loss (counts toward totalLosses). */
  readonly isLoss: boolean;
  readonly lossMagnitude: number;
  readonly profitMagnitude: number;
  /** true only for a losing non-liquidation exit (triggers the pause). */
  readonly pauseAfterLoss: boolean;
}

function settleGridTrade(
  side: "long" | "short",
  exitPrice: number,
  entryPrice: number,
  capital: number,
  isLiquidation: boolean,
  exitFee: number,
  leverage: number,
  positionFraction: number,
): GridSettlement {
  const pricePnl =
    side === "long"
      ? (exitPrice - entryPrice) / entryPrice
      : (entryPrice - exitPrice) / entryPrice;
  const net = pricePnl - exitFee;
  const leveragedReturn = isLiquidation ? -1 : net * leverage;
  const equityReturn = isLiquidation
    ? -positionFraction
    : positionFraction * leveragedReturn;
  const win = !isLiquidation && net >= 0;
  return {
    capital: Math.max(0, capital * (1 + equityReturn)),
    leveragedReturn,
    pnlQuote: capital * equityReturn,
    win,
    isLoss: isLiquidation || net < 0,
    lossMagnitude: Math.abs(leveragedReturn),
    profitMagnitude: leveragedReturn,
    pauseAfterLoss: !isLiquidation && net < 0,
  };
}

export function runGridBacktest(
  candles: readonly CandleLike[],
  options: GridOptions,
): GridResult {
  const runtime = gridRuntime(options);
  let capital = options.initialCapital;
  let peak = capital;
  let maxDrawdown = 0;
  let positionSize = 0;
  let entryPrice = 0;
  let entryBar = 0;
  let totalWins = 0;
  let totalLosses = 0;
  let grossProfit = 0;
  let grossLoss = 0;
  const trades: GridTrade[] = [];
  let paused = 0;
  let fillSeed = options.fillSeed ?? 12345;
  // mulberry32: deterministic fill decisions so a given fillSeed reproduces an
  // identical trade sequence (stress runs are comparable, not random per run).
  const fillRng = (): number => {
    fillSeed |= 0;
    fillSeed = (fillSeed + 0x6d2b79f5) | 0;
    let t = Math.imul(fillSeed ^ (fillSeed >>> 15), fillSeed | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  // Decide whether a touched limit fills. With defaults (prob 1, no adverse
  // selection) this is always true — the original behavior, RNG untouched.
  // Adverse selection: a bar that CLOSES through the level (price grinding
  // through your queue — the loss-prone case) fills for certain; a wick that
  // recovers (win-prone) only fills with the base maker probability.
  const fillsAtLevel = (
    candle: CandleLike,
    level: number,
    side: "long" | "short",
  ): boolean => {
    if (runtime.makerFillProb >= 1 && !runtime.adverseSelection) return true;
    const adverse =
      side === "long" ? candle.close < level : candle.close > level;
    const prob =
      runtime.adverseSelection && adverse ? 1 : runtime.makerFillProb;
    return fillRng() < prob;
  };
  const statsProvider =
    runtime.chopGateAdxThreshold > 0 ? cachedStatsProvider(candles) : null;

  const startIndex = Math.max(options.trendFilterPeriod, 1);
  for (let i = startIndex; i < candles.length; i++) {
    const c = candles[i];
    const trend = sma(candles, i, options.trendFilterPeriod);
    if (trend === null) continue;

    capital = Math.max(0, capital);
    peak = Math.max(peak, capital);
    const dd = peak > 0 ? (peak - capital) / peak : 0;
    if (dd > maxDrawdown) maxDrawdown = dd;

    if (paused > 0) {
      paused--;
      continue;
    }

    const mid = c.open;
    const step = mid * (options.gridStepPct / 100);
    const slippage = 1 + options.slippageBps / 10000;

    if (positionSize === 0) {
      const entry = tryEnterGridPosition(
        i,
        c,
        trend,
        mid,
        step,
        slippage,
        runtime,
        statsProvider,
        options.entryDirectionByBar,
        fillsAtLevel,
      );
      if (entry !== null) {
        positionSize = entry.positionSize;
        entryPrice = entry.entryPrice;
        entryBar = entry.entryBar;
      }
      continue;
    }

    const closeTrade = (
      exitPrice: number,
      exitSide: "long" | "short",
      exitReason: "target" | "stop" | "liquidation",
    ): void => {
      const isLiquidation = exitReason === "liquidation";
      const settled = settleGridTrade(
        exitSide,
        exitPrice,
        entryPrice,
        capital,
        isLiquidation,
        exitReason === "target" ? runtime.targetFee : runtime.stopFee,
        runtime.leverage,
        runtime.positionFraction,
      );
      capital = settled.capital;
      if (settled.isLoss) {
        totalLosses++;
        grossLoss += settled.lossMagnitude;
      } else {
        totalWins++;
        grossProfit += settled.profitMagnitude;
      }
      trades.push({
        side: exitSide,
        entryBar,
        exitBar: i,
        entryPrice,
        exitPrice,
        pnlPct: settled.leveragedReturn,
        pnlQuote: settled.pnlQuote,
        win: settled.win,
        isLiquidation,
      });
      positionSize = 0;
      // Pause only after a losing non-liquidation exit ("losing stop-out"
      // per the option contract); winning target exits and liquidations must
      // not suppress subsequent entries, or the funnel evidence would
      // conflate loss-pauses with a genuine edge.
      paused = settled.pauseAfterLoss ? options.gridPauseAfterLossBars : 0;
    };

    const exit =
      positionSize > 0
        ? longGridExitDecision(c, entryPrice, step, slippage, options, runtime)
        : shortGridExitDecision(
            c,
            entryPrice,
            step,
            slippage,
            options,
            runtime,
          );
    if (exit !== null) {
      closeTrade(exit.exitPrice, exit.side, exit.exitReason);
    }
  }

  // Mark any open grid inventory to market at the final observable close so
  // the backtest doesn't silently erase an adverse open position at the
  // boundary (inflating returns at OOS / walk-forward edges).
  if (positionSize !== 0) {
    const lastClose = candles[candles.length - 1].close;
    const exitSide = positionSize > 0 ? "long" : "short";
    const settled = settleGridTrade(
      exitSide,
      lastClose,
      entryPrice,
      capital,
      false,
      runtime.stopFee,
      runtime.leverage,
      runtime.positionFraction,
    );
    capital = settled.capital;
    peak = Math.max(peak, capital);
    const dd = peak > 0 ? (peak - capital) / peak : 0;
    if (dd > maxDrawdown) maxDrawdown = dd;
    if (settled.isLoss) {
      totalLosses++;
      grossLoss += settled.lossMagnitude;
    } else {
      totalWins++;
      grossProfit += settled.profitMagnitude;
    }
    trades.push({
      side: exitSide,
      entryBar,
      exitBar: candles.length - 1,
      entryPrice,
      exitPrice: lastClose,
      pnlPct: settled.leveragedReturn,
      pnlQuote: settled.pnlQuote,
      win: settled.win,
      isLiquidation: false,
    });
    positionSize = 0;
  }

  const totalTrades = totalWins + totalLosses;
  return {
    totalReturnPct:
      ((capital - options.initialCapital) / options.initialCapital) * 100,
    maxDrawdownPct: maxDrawdown * 100,
    winRate: totalTrades > 0 ? (totalWins / totalTrades) * 100 : 0,
    totalTrades,
    profitFactor:
      grossLoss > 0 ? grossProfit / grossLoss : grossProfit > 0 ? Infinity : 0,
    trades,
  };
}

export function findBestGridParams(
  trainCandles: readonly CandleLike[],
  searchSpace: GridSearchSpace,
  baseOptions: Omit<
    GridOptions,
    "gridStepPct" | "gridMaxGrids" | "gridPauseAfterLossBars"
  >,
): GridOptions & { result: GridResult } {
  let best: (GridOptions & { result: GridResult }) | null = null;

  for (const gridStepPct of searchSpace.gridStepPct) {
    for (const gridMaxGrids of searchSpace.gridMaxGrids) {
      for (const gridPauseAfterLossBars of searchSpace.gridPauseAfterLossBars) {
        const options: GridOptions = {
          ...baseOptions,
          gridStepPct,
          gridMaxGrids,
          gridPauseAfterLossBars,
        };
        const result = runGridBacktest(trainCandles, options);
        if (!best || result.totalReturnPct > best.result.totalReturnPct) {
          best = { ...options, result };
        }
      }
    }
  }

  if (!best) {
    throw new Error("Grid search space produced no parameter combinations.");
  }

  return best;
}

export function runGridWalkForward(
  candles: readonly CandleLike[],
  options: {
    readonly trainWindow: number;
    readonly testWindow: number;
    readonly initialCapital: number;
    readonly searchSpace: GridSearchSpace;
    readonly baseOptions: Omit<
      GridOptions,
      | "gridStepPct"
      | "gridMaxGrids"
      | "gridPauseAfterLossBars"
      | "initialCapital"
    >;
  },
): GridWalkForwardResult {
  const { trainWindow, testWindow, initialCapital, searchSpace, baseOptions } =
    options;

  const windows: GridWalkForwardWindow[] = [];
  let runningCapital = initialCapital;
  let aggregateMaxDrawdown = 0;
  let totalTrades = 0;
  // Trade-weighted win/loss accumulation across ALL window test slices:
  // sums (not per-window means) so windows with more trades weigh more.
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

    const best = findBestGridParams(trainCandles, searchSpace, {
      ...baseOptions,
      initialCapital: runningCapital,
    });

    const testResult = runGridBacktest(testCandles, {
      ...best,
      initialCapital: runningCapital,
    });

    // Cap a single window's compounding contribution to ±100% so an all-in
    // sizing artifact in one outlier window cannot blow the aggregate return
    // up to non-physical values (observed 9e17%) that corrupt ranking.
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

    // Per-window win/loss means (undefined when a window has no winners or
    // no losers) — the funnel gate needs the asymmetry per validation slice,
    // not just the aggregate.
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
