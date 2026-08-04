import { describe, expect, it } from "bun:test";
import type { CandleLike } from "./types.js";
import {
  findBestGridParams,
  runGridBacktest,
  runGridWalkForward,
} from "./grid.js";

function makeOscillatingCandles(
  count: number,
  mid = 100.5,
  amplitude = 0.5,
): CandleLike[] {
  const candles: CandleLike[] = [];
  for (let i = 0; i < count; i++) {
    const side = i % 2 === 0 ? 1 : -1;
    const high = mid + amplitude * side + 0.05;
    const low = mid - amplitude * side - 0.05;
    const open = mid - (amplitude / 2) * side;
    const close = mid + (amplitude / 2) * side;
    candles.push({
      open,
      high,
      low,
      close,
      volume: 1,
      timestamp: new Date(i * 15 * 60 * 1000),
    });
  }
  return candles;
}

describe("runGridBacktest", () => {
  it("captures trades on an oscillating series and produces profit factor > 1", () => {
    const candles = makeOscillatingCandles(300);
    const result = runGridBacktest(candles, {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 96,
      leverage: 1,
    });

    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.profitFactor).toBeGreaterThan(1);
    expect(result.totalReturnPct).toBeGreaterThan(0);
  });

  it("returns zero trades for a flat series with no grid touches", () => {
    const candles: CandleLike[] = Array.from({ length: 200 }, (_, i) => ({
      open: 100,
      high: 100.1,
      low: 99.9,
      close: 100,
      volume: 1,
      timestamp: new Date(i * 15 * 60 * 1000),
    }));

    const result = runGridBacktest(candles, {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 96,
      leverage: 1,
    });

    expect(result.totalTrades).toBe(0);
    expect(result.totalReturnPct).toBe(0);
  });

  it("chop gate blocks new entries while the market is trending", () => {
    // Strong directional drift with pullback wobble: grids would touch, but
    // ADX is high, so a gated engine must stay out entirely.
    const candles: CandleLike[] = [];
    let price = 100;
    for (let i = 0; i < 400; i++) {
      const drift = i < 30 ? 0 : 0.005;
      const wobble = Math.sin(i / 2) * 0.004;
      price = price * (1 + drift + wobble * 0.2);
      const open = price * (1 - 0.004);
      candles.push({
        open,
        high: price * 1.006,
        low: open * 0.994,
        close: price,
        volume: 1,
        timestamp: new Date(i * 15 * 60 * 1000),
      });
    }

    const base = {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 96,
      leverage: 1,
    };
    const ungated = runGridBacktest(candles, base);
    const gated = runGridBacktest(candles, {
      ...base,
      chopGateAdxThreshold: 25,
    });

    expect(ungated.totalTrades).toBeGreaterThan(0);
    expect(gated.totalTrades).toBe(0);
  });

  it("chop gate keeps trading in an oscillating (low-ADX) market", () => {
    const candles = makeOscillatingCandles(300);
    const result = runGridBacktest(candles, {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 96,
      leverage: 1,
      chopGateAdxThreshold: 25,
    });

    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.profitFactor).toBeGreaterThan(1);
  });

  it("records a trade list with bar indices, prices, and pnl", () => {
    const candles = makeOscillatingCandles(300);
    const result = runGridBacktest(candles, {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 96,
      leverage: 1,
    });

    expect(result.trades.length).toBe(result.totalTrades);
    for (const t of result.trades) {
      expect(t.exitBar).toBeGreaterThanOrEqual(t.entryBar);
      expect(t.entryPrice).toBeGreaterThan(0);
      expect(t.exitPrice).toBeGreaterThan(0);
      expect(typeof t.pnlPct).toBe("number");
      expect(t.win).toBe(t.pnlPct > 0);
      expect(["long", "short"]).toContain(t.side);
      expect(typeof t.isLiquidation).toBe("boolean");
    }
    const wins = result.trades.filter((t) => t.win).length;
    expect(wins / result.trades.length).toBeCloseTo(result.winRate / 100, 2);
  });

  const sizingOpts = {
    gridStepPct: 0.5,
    gridMaxGrids: 2,
    gridPauseAfterLossBars: 0,
    feePct: 0.04,
    slippageBps: 1,
    initialCapital: 20,
    trendFilterPeriod: 96,
    leverage: 1,
  };

  it("positionFraction defaults to 1 and is backward compatible", () => {
    const candles = makeOscillatingCandles(300);
    const baseline = runGridBacktest(candles, sizingOpts);
    const explicit = runGridBacktest(candles, {
      ...sizingOpts,
      positionFraction: 1,
    });
    expect(explicit.totalTrades).toBe(baseline.totalTrades);
    expect(explicit.totalReturnPct).toBeCloseTo(baseline.totalReturnPct, 8);
    expect(explicit.maxDrawdownPct).toBeCloseTo(baseline.maxDrawdownPct, 8);
  });

  it("positionFraction scales return and drawdown down, not trade count", () => {
    const candles = makeOscillatingCandles(400);
    const full = runGridBacktest(candles, sizingOpts);
    const half = runGridBacktest(candles, {
      ...sizingOpts,
      positionFraction: 0.5,
    });
    expect(full.totalReturnPct).toBeGreaterThan(0);
    expect(half.totalTrades).toBe(full.totalTrades);
    expect(half.totalReturnPct).toBeLessThan(full.totalReturnPct);
    expect(half.totalReturnPct).toBeGreaterThan(0);
    expect(half.maxDrawdownPct).toBeLessThanOrEqual(full.maxDrawdownPct);
  });

  it("positionFraction leaves win rate, profit factor, and expectancy invariant", () => {
    const candles = makeOscillatingCandles(400);
    const full = runGridBacktest(candles, sizingOpts);
    const half = runGridBacktest(candles, {
      ...sizingOpts,
      positionFraction: 0.5,
    });
    expect(half.winRate).toBeCloseTo(full.winRate, 6);
    expect(half.profitFactor).toBeCloseTo(full.profitFactor, 6);
    expect(half.totalTrades).toBeGreaterThan(0);
    const mean = (r: typeof full): number =>
      r.trades.reduce((s, t) => s + t.pnlPct, 0) / r.trades.length;
    expect(mean(half)).toBeCloseTo(mean(full), 8);
  });
});

describe("findBestGridParams", () => {
  it("selects the highest-returning parameter combo", () => {
    const candles = makeOscillatingCandles(300);
    const best = findBestGridParams(
      candles,
      {
        gridStepPct: [0.3, 0.5],
        gridMaxGrids: [2, 3],
        gridPauseAfterLossBars: [0],
      },
      {
        feePct: 0.04,
        slippageBps: 1,
        initialCapital: 20,
        trendFilterPeriod: 96,
        leverage: 1,
      },
    );

    expect(best.gridStepPct).toBeOneOf([0.3, 0.5]);
    expect(best.gridMaxGrids).toBeOneOf([2, 3]);
    expect(best.result.totalTrades).toBeGreaterThan(0);
  });
});

describe("runGridWalkForward", () => {
  it("produces profitable windows on an oscillating series", () => {
    const candles = makeOscillatingCandles(800);
    const result = runGridWalkForward(candles, {
      trainWindow: 200,
      testWindow: 150,
      initialCapital: 20,
      searchSpace: {
        gridStepPct: [0.3, 0.5],
        gridMaxGrids: [2, 3],
        gridPauseAfterLossBars: [0],
      },
      baseOptions: {
        feePct: 0.04,
        slippageBps: 1,
        trendFilterPeriod: 48,
        leverage: 1,
      },
    });

    expect(result.windows.length).toBeGreaterThan(0);
    expect(result.profitableWindowsPct).toBeGreaterThan(0);
  });
});
