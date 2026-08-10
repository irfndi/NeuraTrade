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
}

export interface GridWalkForwardResult {
  readonly windows: GridWalkForwardWindow[];
  readonly aggregateReturnPct: number;
  readonly profitableWindowsPct: number;
  readonly maxDrawdownPct: number;
  readonly totalTrades: number;
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

export function runGridBacktest(
  candles: readonly CandleLike[],
  options: GridOptions,
): GridResult {
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
  const leverage = Math.max(1, options.leverage ?? 1);
  const chopGateAdxThreshold = Math.max(0, options.chopGateAdxThreshold ?? 0);
  const positionFraction = Math.max(
    0,
    Math.min(1, options.positionFraction ?? 1),
  );
  const makerFillProb = Math.max(0, Math.min(1, options.makerFillProb ?? 1));
  const adverseSelection = options.adverseSelection ?? false;
  const makerFeePerSide = options.feePct / 100;
  const takerFeePerSide = (options.takerExitFeePct ?? options.feePct) / 100;
  const targetFee = makerFeePerSide * 2;
  const stopFee = makerFeePerSide + takerFeePerSide;
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
    if (makerFillProb >= 1 && !adverseSelection) return true;
    const adverse =
      side === "long" ? candle.close < level : candle.close > level;
    const prob = adverseSelection && adverse ? 1 : makerFillProb;
    return fillRng() < prob;
  };
  const statsProvider =
    chopGateAdxThreshold > 0
      ? // The provider's timeframe argument is currently unused (the
        // annualized-volatility field is always 0, matching batch behavior).
        makeCausalSymbolStats(candles, "15m")
      : null;

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
      // Chop gate: trending markets are where grid inventory gets run over;
      // sit out until ADX says the market is ranging again.
      if (
        statsProvider !== null &&
        statsProvider(i).adx14 >= chopGateAdxThreshold
      ) {
        continue;
      }
      const buyLevel = mid - step;
      const sellLevel = mid + step;
      const onlyWithTrend = options.onlyWithTrend ?? false;
      const allowLong = !onlyWithTrend || c.close > trend;
      const allowShort = !onlyWithTrend || c.close < trend;
      if (allowLong && c.low <= buyLevel && fillsAtLevel(c, buyLevel, "long")) {
        entryPrice = buyLevel * slippage;
        positionSize = 1;
        entryBar = i;
      } else if (
        allowShort &&
        c.high >= sellLevel &&
        fillsAtLevel(c, sellLevel, "short")
      ) {
        entryPrice = sellLevel / slippage;
        positionSize = -1;
        entryBar = i;
      }
      continue;
    }

    const closeTrade = (
      exitPrice: number,
      exitSide: "long" | "short",
      exitReason: "target" | "stop" | "liquidation",
    ): void => {
      const isLiquidation = exitReason === "liquidation";
      const fee = exitReason === "target" ? targetFee : stopFee;
      const pricePnl =
        exitSide === "long"
          ? (exitPrice - entryPrice) / entryPrice
          : (entryPrice - exitPrice) / entryPrice;
      const net = pricePnl - fee;
      const leveragedReturn = isLiquidation ? -1 : net * leverage;
      const capitalBefore = capital;
      // positionFraction sizes the EQUITY allocated to the trade. Per-trade
      // edge metrics (win / PF / expectancy from pnlPct) stay sizing-invariant;
      // only the equity curve and drawdown scale with the fraction. At the
      // default fraction = 1 this reproduces the original all-in behavior.
      const equityReturn = isLiquidation
        ? -positionFraction
        : positionFraction * leveragedReturn;
      capital = Math.max(0, capitalBefore * (1 + equityReturn));
      const win = !isLiquidation && net >= 0;
      if (isLiquidation || net < 0) {
        totalLosses++;
        grossLoss += Math.abs(leveragedReturn);
      } else {
        totalWins++;
        grossProfit += leveragedReturn;
      }
      trades.push({
        side: exitSide,
        entryBar,
        exitBar: i,
        entryPrice,
        exitPrice,
        pnlPct: leveragedReturn,
        pnlQuote: capitalBefore * equityReturn,
        win,
        isLiquidation,
      });
      positionSize = 0;
      // Pause only after a losing non-liquidation exit ("losing stop-out"
      // per the option contract); winning target exits and liquidations must
      // not suppress subsequent entries, or the funnel evidence would
      // conflate loss-pauses with a genuine edge.
      paused =
        isLiquidation || net >= 0 ? 0 : options.gridPauseAfterLossBars;
    };

    const targetRatio = options.targetRatio ?? 1;
    if (positionSize > 0) {
      const target = entryPrice + step * targetRatio;
      const stop = entryPrice - step * options.gridMaxGrids;
      const liq = liquidationPrice("long", entryPrice, leverage);
      if (liq > 0 && c.low <= liq) {
        closeTrade(liq * slippage, "long", "liquidation");
      } else if (c.high >= target) {
        closeTrade(target / slippage, "long", "target");
      } else if (c.low <= stop) {
        closeTrade(stop * slippage, "long", "stop");
      }
    } else {
      const target = entryPrice - step * targetRatio;
      const stop = entryPrice + step * options.gridMaxGrids;
      const liq = liquidationPrice("short", entryPrice, leverage);
      if (liq > 0 && c.high >= liq) {
        closeTrade(liq / slippage, "short", "liquidation");
      } else if (c.low <= target) {
        closeTrade(target * slippage, "short", "target");
      } else if (c.high >= stop) {
        closeTrade(stop / slippage, "short", "stop");
      }
    }
  }

  // Mark any open grid inventory to market at the final observable close so
  // the backtest doesn't silently erase an adverse open position at the
  // boundary (inflating returns at OOS / walk-forward edges).
  if (positionSize !== 0) {
    const lastClose = candles[candles.length - 1].close;
    const exitSide = positionSize > 0 ? "long" : "short";
    const exitPrice = lastClose;
    const fee = stopFee;
    const pricePnl =
      exitSide === "long"
        ? (exitPrice - entryPrice) / entryPrice
        : (entryPrice - exitPrice) / entryPrice;
    const net = pricePnl - fee;
    const capitalBefore = capital;
    const leveragedReturn = net * leverage;
    const equityReturn = positionFraction * leveragedReturn;
    capital = Math.max(0, capitalBefore * (1 + equityReturn));
    peak = Math.max(peak, capital);
    const dd = peak > 0 ? (peak - capital) / peak : 0;
    if (dd > maxDrawdown) maxDrawdown = dd;
    const win = net >= 0;
    if (net < 0) {
      totalLosses++;
      grossLoss += Math.abs(leveragedReturn);
    } else {
      totalWins++;
      grossProfit += leveragedReturn;
    }
    trades.push({
      side: exitSide,
      entryBar,
      exitBar: candles.length - 1,
      entryPrice,
      exitPrice,
      pnlPct: leveragedReturn,
      pnlQuote: capitalBefore * equityReturn,
      win,
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

    runningCapital *= 1 + testResult.totalReturnPct / 100;
    aggregateMaxDrawdown = Math.max(
      aggregateMaxDrawdown,
      testResult.maxDrawdownPct,
    );
    totalTrades += testResult.totalTrades;

    windows.push({
      trainStartIndex: start,
      trainEndIndex: start + trainWindow,
      testStartIndex: start + trainWindow,
      testEndIndex: start + trainWindow + testWindow,
      params: best,
      testReturnPct: testResult.totalReturnPct,
      testMaxDrawdownPct: testResult.maxDrawdownPct,
      testTrades: testResult.totalTrades,
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
  };
}
