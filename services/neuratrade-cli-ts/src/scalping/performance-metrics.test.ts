import { describe, expect, it } from "bun:test";
import {
  computePerformanceMetrics,
  robustnessScore,
  type BacktestMetrics,
} from "./performance-metrics.js";
import type { BacktestResult, BacktestTrade } from "./backtest.js";

function makeTrade(overrides: Partial<BacktestTrade> = {}): BacktestTrade {
  const entryTime = overrides.entryTime ?? new Date(Date.UTC(2025, 0, 1, 0, 0));
  const exitTime = overrides.exitTime ?? new Date(Date.UTC(2025, 0, 1, 1, 0));
  return {
    id: "t1",
    symbol: "BTC/USDT",
    side: "long",
    entryTime,
    exitTime,
    entryPrice: 100,
    exitPrice: 102,
    pnl: 2,
    pnlPct: 2,
    netPnl: 2,
    exitReason: "take_profit",
    initialRiskPct: 0.01,
    fillType: "taker",
    entryFeePct: 0.1,
    exitFeePct: 0.1,
    ...overrides,
  };
}

function compute(
  trades: readonly BacktestTrade[],
  overrides: Partial<Parameters<typeof computePerformanceMetrics>[0]> = {},
): BacktestMetrics {
  return computePerformanceMetrics({
    trades,
    initialCapital: 10_000,
    maxDrawdownPct: 5,
    totalReturnPct: 10,
    candleSpanMs: 24 * 3_600_000,
    ...overrides,
  });
}

describe("computePerformanceMetrics", () => {
  it("returns zero metrics for no trades", () => {
    const m = compute([]);
    expect(m.profitFactor).toBe(0);
    expect(m.expectancy).toBe(0);
    expect(m.averageRMultiple).toBe(0);
    expect(m.sortinoRatio).toBe(0);
    expect(m.calmarRatio).toBe(0);
    expect(m.maxConsecutiveLosses).toBe(0);
    expect(m.averageTradeDurationHours).toBe(0);
    expect(m.timeInMarketPct).toBe(0);
  });

  it("computes profit factor and expectancy for a mixed set", () => {
    const trades: BacktestTrade[] = [
      makeTrade({ pnl: 100, pnlPct: 1, initialRiskPct: 0.005 }),
      makeTrade({ pnl: 100, pnlPct: 1, initialRiskPct: 0.005 }),
      makeTrade({ pnl: -50, pnlPct: -0.5, initialRiskPct: 0.005 }),
      makeTrade({ pnl: -50, pnlPct: -0.5, initialRiskPct: 0.005 }),
    ];
    const m = compute(trades);
    expect(m.profitFactor).toBeCloseTo(2, 5);
    expect(m.expectancy).toBeCloseTo(0.25, 5);
    expect(m.maxConsecutiveLosses).toBe(2);
  });

  it("returns infinity profit factor when there are no losses", () => {
    const trades: BacktestTrade[] = [
      makeTrade({ pnl: 100, pnlPct: 1 }),
      makeTrade({ pnl: 200, pnlPct: 2 }),
    ];
    const m = compute(trades);
    expect(m.profitFactor).toBe(Number.POSITIVE_INFINITY);
  });

  it("computes average R-multiple from pnlPct and initial risk", () => {
    const trades: BacktestTrade[] = [
      makeTrade({ pnlPct: 2, initialRiskPct: 0.01 }),
      makeTrade({ pnlPct: -1, initialRiskPct: 0.01 }),
    ];
    const m = compute(trades);
    expect(m.averageRMultiple).toBeCloseTo(0.5, 5);
  });

  it("computes Sortino ratio using downside returns", () => {
    const trades: BacktestTrade[] = [
      makeTrade({ pnlPct: 2 }),
      makeTrade({ pnlPct: 2 }),
      makeTrade({ pnlPct: -1 }),
    ];
    const m = compute(trades);
    expect(m.sortinoRatio).toBeGreaterThan(0);
  });

  it("computes Calmar ratio from total return and max drawdown", () => {
    const m = compute([makeTrade()], { totalReturnPct: 20, maxDrawdownPct: 5 });
    expect(m.calmarRatio).toBeCloseTo(4, 5);
  });

  it("computes time in market from trade durations", () => {
    const entry = new Date(Date.UTC(2025, 0, 1, 0, 0));
    const exit = new Date(Date.UTC(2025, 0, 1, 6, 0));
    const m = compute([makeTrade({ entryTime: entry, exitTime: exit })], {
      candleSpanMs: 24 * 3_600_000,
    });
    expect(m.averageTradeDurationHours).toBeCloseTo(6, 5);
    expect(m.timeInMarketPct).toBeCloseTo(25, 5);
  });

  it("handles zero candle span safely", () => {
    const m = compute([makeTrade()], { candleSpanMs: 0 });
    expect(m.timeInMarketPct).toBe(0);
  });

  it("tracks max consecutive losses across streaks", () => {
    const trades: BacktestTrade[] = [
      makeTrade({ pnl: -1 }),
      makeTrade({ pnl: -1 }),
      makeTrade({ pnl: 1 }),
      makeTrade({ pnl: -1 }),
      makeTrade({ pnl: -1 }),
      makeTrade({ pnl: -1 }),
      makeTrade({ pnl: 1 }),
    ];
    const m = compute(trades);
    expect(m.maxConsecutiveLosses).toBe(3);
  });
});

function makeResult(overrides: Partial<BacktestResult> = {}): BacktestResult {
  return {
    symbol: "BTC/USDT",
    totalTrades: 30,
    winningTrades: 15,
    losingTrades: 15,
    winRate: 0.5,
    totalReturnPct: 0,
    maxDrawdownPct: 5,
    sharpeRatio: 0,
    trades: [],
    totalFeesPaid: 0,
    totalFundingCost: 0,
    benchmarkReturnPct: 0,
    metrics: {
      profitFactor: 1,
      expectancy: 0,
      averageRMultiple: 0,
      sortinoRatio: 0,
      calmarRatio: 0,
      maxConsecutiveLosses: 0,
      averageTradeDurationHours: 0,
      timeInMarketPct: 0,
    },
    robustnessScore: 0,
    ...overrides,
  };
}

describe("robustnessScore", () => {
  it("returns 0 for a flat, baseline result", () => {
    const score = robustnessScore(makeResult({ maxDrawdownPct: 0 }));
    expect(score).toBe(0);
  });

  it("rewards positive returns", () => {
    const positive = makeResult({ totalReturnPct: 10 });
    const negative = makeResult({ totalReturnPct: -10 });
    expect(robustnessScore(positive)).toBeGreaterThan(
      robustnessScore(negative),
    );
  });

  it("penalizes drawdowns", () => {
    const lowDrawdown = makeResult({ totalReturnPct: 5, maxDrawdownPct: 2 });
    const highDrawdown = makeResult({ totalReturnPct: 5, maxDrawdownPct: 20 });
    expect(robustnessScore(lowDrawdown)).toBeGreaterThan(
      robustnessScore(highDrawdown),
    );
  });

  it("rewards higher Sharpe ratios", () => {
    const betterSharpe = makeResult({ sharpeRatio: 1.5 });
    const worseSharpe = makeResult({ sharpeRatio: -0.5 });
    expect(robustnessScore(betterSharpe)).toBeGreaterThan(
      robustnessScore(worseSharpe),
    );
  });

  it("rewards profit factor above 1", () => {
    const profitable = makeResult({
      metrics: { ...makeResult().metrics, profitFactor: 2 },
    });
    const breakeven = makeResult({
      metrics: { ...makeResult().metrics, profitFactor: 1 },
    });
    expect(robustnessScore(profitable)).toBeGreaterThan(
      robustnessScore(breakeven),
    );
  });

  it("penalizes small sample sizes", () => {
    const smallSample = makeResult({ totalTrades: 5 });
    const largeSample = makeResult({ totalTrades: 100 });
    expect(robustnessScore(smallSample)).toBeLessThan(
      robustnessScore(largeSample),
    );
  });

  it("clamps the score to [-100, 100]", () => {
    const extreme = makeResult({
      totalReturnPct: 500,
      sharpeRatio: 50,
      metrics: { ...makeResult().metrics, profitFactor: 100 },
    });
    expect(robustnessScore(extreme)).toBeLessThanOrEqual(100);
    expect(robustnessScore(extreme)).toBeGreaterThan(0);

    const terrible = makeResult({
      totalReturnPct: -500,
      maxDrawdownPct: 500,
      sharpeRatio: -50,
    });
    expect(robustnessScore(terrible)).toBeGreaterThanOrEqual(-100);
    expect(robustnessScore(terrible)).toBeLessThan(0);
  });
});
