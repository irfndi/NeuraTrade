import { describe, expect, it } from "bun:test";
import {
  computePerformanceMetrics,
  type BacktestMetrics,
} from "./performance-metrics.js";
import type { BacktestTrade } from "./backtest.js";

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
    exitReason: "take_profit",
    initialRiskPct: 0.01,
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
