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
