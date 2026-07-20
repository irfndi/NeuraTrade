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
      if (allowLong && c.low <= buyLevel) {
        entryPrice = buyLevel * slippage;
        positionSize = 1;
        entryBar = i;
      } else if (allowShort && c.high >= sellLevel) {
        entryPrice = sellLevel / slippage;
        positionSize = -1;
        entryBar = i;
      }
      continue;
    }

    const fee = (options.feePct / 100) * 2;
    const closeTrade = (
      exitPrice: number,
      exitSide: "long" | "short",
      isLiquidation: boolean,
    ): void => {
      const pricePnl =
        exitSide === "long"
          ? (exitPrice - entryPrice) / entryPrice
          : (entryPrice - exitPrice) / entryPrice;
      const net = pricePnl - fee;
      const leveragedReturn = isLiquidation ? -1 : net * leverage;
      const capitalBefore = capital;
      const rawCapitalAfter = capital * (1 + leveragedReturn);
      capital = isLiquidation ? 0 : Math.max(0, rawCapitalAfter);
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
        pnlQuote: isLiquidation
          ? -capitalBefore
          : capitalBefore * leveragedReturn,
        win,
        isLiquidation,
      });
      positionSize = 0;
      paused = isLiquidation ? 0 : options.gridPauseAfterLossBars;
    };

    const targetRatio = options.targetRatio ?? 1;
    if (positionSize > 0) {
      const target = entryPrice + step * targetRatio;
      const stop = entryPrice - step * options.gridMaxGrids;
      const liq = liquidationPrice("long", entryPrice, leverage);
      if (liq > 0 && c.low <= liq) {
        closeTrade(liq * slippage, "long", true);
      } else if (c.high >= target) {
        closeTrade(target / slippage, "long", false);
      } else if (c.low <= stop) {
        closeTrade(stop * slippage, "long", false);
      }
    } else {
      const target = entryPrice - step * targetRatio;
      const stop = entryPrice + step * options.gridMaxGrids;
      const liq = liquidationPrice("short", entryPrice, leverage);
      if (liq > 0 && c.high >= liq) {
        closeTrade(liq / slippage, "short", true);
      } else if (c.low <= target) {
        closeTrade(target * slippage, "short", false);
      } else if (c.high >= stop) {
        closeTrade(stop / slippage, "short", false);
      }
    }
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
