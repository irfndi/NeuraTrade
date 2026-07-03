import { calculateSharpe } from "./backtest.js";
import type { BacktestResult } from "./backtest.js";
import type { CandleLike } from "./types.js";
import type { SelectArgs, SelectResult } from "../cli/scalp.js";

export interface WalkForwardWindowResult {
  readonly trainStartIndex: number;
  readonly trainEndIndex: number;
  readonly testStartIndex: number;
  readonly testEndIndex: number;
  readonly params: SelectResult["params"];
  readonly testReturnPct: number;
  readonly testMaxDrawdownPct: number;
  readonly testTrades: number;
  readonly testResult: BacktestResult;
}

export interface WalkForwardResult {
  readonly windows: WalkForwardWindowResult[];
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly sharpeRatio: number;
  readonly profitableWindowsPct: number;
  readonly avgTradesPerWindow: number;
  readonly avgWindowReturnPct: number;
}

export interface WalkForwardOptions {
  readonly symbol: string;
  readonly exchange: string;
  readonly candles: readonly CandleLike[];
  readonly trainWindow: number;
  readonly testWindow: number;
  readonly initialCapital: number;
  readonly args: SelectArgs;
  readonly selectBestForSymbol: (
    symbol: string,
    candles: readonly CandleLike[],
    args: SelectArgs,
    exchange: string,
  ) => SelectResult | null;
  readonly runSelectBacktest: (
    symbol: string,
    candles: readonly CandleLike[],
    args: SelectArgs,
    exchange: string,
    params: SelectResult["params"],
  ) => BacktestResult;
}

function aggregateCombinedEquity(
  windows: readonly WalkForwardWindowResult[],
  initialCapital: number,
): { totalReturnPct: number; maxDrawdownPct: number } {
  if (windows.length === 0 || initialCapital <= 0) {
    return { totalReturnPct: 0, maxDrawdownPct: 0 };
  }

  let capital = initialCapital;
  let peak = capital;
  let maxDrawdown = 0;

  for (const w of windows) {
    const windowStartCapital = capital;
    const scale = windowStartCapital / initialCapital;
    for (const t of w.testResult.trades) {
      const scaledNetPnl = t.netPnl * scale;
      capital += scaledNetPnl;
      if (capital > peak) peak = capital;
      const dd = peak > 0 ? (peak - capital) / peak : 0;
      if (dd > maxDrawdown) maxDrawdown = dd;
    }
  }

  const totalReturnPct = ((capital - initialCapital) / initialCapital) * 100;
  return { totalReturnPct, maxDrawdownPct: maxDrawdown * 100 };
}

export function runWalkForward(options: WalkForwardOptions): WalkForwardResult {
  const {
    symbol,
    exchange,
    candles,
    trainWindow,
    testWindow,
    initialCapital,
    args,
    selectBestForSymbol,
    runSelectBacktest,
  } = options;

  const windows: WalkForwardWindowResult[] = [];

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

    const selected = selectBestForSymbol(symbol, trainCandles, args, exchange);
    if (!selected) continue;

    const testResult = runSelectBacktest(
      symbol,
      testCandles,
      args,
      exchange,
      selected.params,
    );

    windows.push({
      trainStartIndex: start,
      trainEndIndex: start + trainWindow,
      testStartIndex: start + trainWindow,
      testEndIndex: start + trainWindow + testWindow,
      params: selected.params,
      testReturnPct: testResult.totalReturnPct,
      testMaxDrawdownPct: testResult.maxDrawdownPct,
      testTrades: testResult.totalTrades,
      testResult,
    });
  }

  const windowReturns = windows.map((w) => w.testReturnPct);
  const avgWindowReturnPct =
    windows.length > 0
      ? windowReturns.reduce((sum, r) => sum + r, 0) / windows.length
      : 0;
  const profitableCount = windows.filter((w) => w.testReturnPct > 0).length;
  const profitableWindowsPct =
    windows.length > 0 ? (profitableCount / windows.length) * 100 : 0;
  const avgTradesPerWindow =
    windows.length > 0
      ? windows.reduce((sum, w) => sum + w.testTrades, 0) / windows.length
      : 0;

  const { totalReturnPct, maxDrawdownPct } = aggregateCombinedEquity(
    windows,
    initialCapital,
  );
  const sharpeRatio = calculateSharpe(windowReturns);

  return {
    windows,
    totalReturnPct,
    maxDrawdownPct,
    sharpeRatio,
    profitableWindowsPct,
    avgTradesPerWindow,
    avgWindowReturnPct,
  };
}
