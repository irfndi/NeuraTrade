import { describe, expect, it } from "bun:test";
import type { CandleLike } from "./types.js";
import {
  findBestLadderGridParams,
  liquidationPrice,
  runLadderGridBacktest,
  runLadderGridWalkForward,
  type LadderOptions,
} from "./ladder-grid.js";

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

function makeRandomWalkCandles(count: number, seed = 7): CandleLike[] {
  let state = seed;
  const rand = (): number => {
    state = (state * 1103515245 + 12345) % 2147483648;
    return state / 2147483648 - 0.5;
  };
  const candles: CandleLike[] = [];
  let price = 100;
  for (let i = 0; i < count; i++) {
    const open = price;
    const close = price * (1 + rand() * 0.01);
    const wick = Math.abs(rand()) * 0.006;
    candles.push({
      open,
      high: Math.max(open, close) * (1 + wick),
      low: Math.min(open, close) * (1 - wick),
      close,
      volume: 1,
      timestamp: new Date(i * 15 * 60 * 1000),
    });
    price = close;
  }
  return candles;
}

/** Strictly downward candles with no high bounce — forces long stop-outs */
function makeTrendingDownCandles(count: number): CandleLike[] {
  const candles: CandleLike[] = [];
  let price = 100;
  for (let i = 0; i < count; i++) {
    const open = price;
    const close = price * 0.995;
    candles.push({
      open,
      high: open * 0.999,
      low: close * 0.995,
      close,
      volume: 1,
      timestamp: new Date(i * 15 * 60 * 1000),
    });
    price = close;
  }
  return candles;
}

describe("liquidationPrice", () => {
  it("returns 0 when leverage <= 1", () => {
    expect(liquidationPrice("long", 100, 1)).toBe(0);
    expect(liquidationPrice("short", 100, 1)).toBe(0);
    expect(liquidationPrice("long", 100, 0.5)).toBe(0);
    expect(liquidationPrice("short", 100, 0)).toBe(0);
  });

  it("calculates long liquidation price correctly", () => {
    // 10x leverage on long: 100 * (1 - 1/10) = 90
    expect(liquidationPrice("long", 100, 10)).toBeCloseTo(90, 6);
    // 2x leverage on long: 100 * (1 - 1/2) = 50
    expect(liquidationPrice("long", 100, 2)).toBeCloseTo(50, 6);
    // 20x leverage on long: 100 * (1 - 1/20) = 95
    expect(liquidationPrice("long", 100, 20)).toBeCloseTo(95, 6);
  });

  it("calculates short liquidation price correctly", () => {
    // 10x leverage on short: 100 * (1 + 1/10) = 110
    expect(liquidationPrice("short", 100, 10)).toBeCloseTo(110, 6);
    // 2x leverage on short: 100 * (1 + 1/2) = 150
    expect(liquidationPrice("short", 100, 2)).toBeCloseTo(150, 6);
    // 20x leverage on short: 100 * (1 + 1/20) = 105
    expect(liquidationPrice("short", 100, 20)).toBeCloseTo(105, 6);
  });
});

describe("runLadderGridBacktest", () => {
  const baseOpts: LadderOptions = {
    rungs: 1,
    gridStepPct: 0.5,
    gridMaxGrids: 2,
    gridPauseAfterLossBars: 0,
    feePct: 0.04,
    slippageBps: 1,
    initialCapital: 100,
    trendFilterPeriod: 0,
    leverage: 1,
  };

  it("handles empty or insufficient candles gracefully without throwing", () => {
    const emptyResult = runLadderGridBacktest([], baseOpts);
    expect(emptyResult.totalTrades).toBe(0);
    expect(emptyResult.totalReturnPct).toBe(0);
    expect(emptyResult.maxDrawdownPct).toBe(0);
    expect(emptyResult.winRate).toBe(0);
    expect(emptyResult.profitFactor).toBe(0);
    expect(emptyResult.trades.length).toBe(0);

    const singleCandle: CandleLike[] = [
      {
        open: 100,
        high: 100.5,
        low: 99.5,
        close: 100,
        volume: 1,
        timestamp: new Date(0),
      },
    ];
    const singleResult = runLadderGridBacktest(singleCandle, {
      ...baseOpts,
      trendFilterPeriod: 10,
    });
    expect(singleResult.totalTrades).toBe(0);
  });

  it("returns zero trades on flat series with no touches", () => {
    const candles: CandleLike[] = Array.from({ length: 100 }, (_, i) => ({
      open: 100,
      high: 100.1,
      low: 99.9,
      close: 100,
      volume: 1,
      timestamp: new Date(i * 15 * 60 * 1000),
    }));
    const result = runLadderGridBacktest(candles, baseOpts);
    expect(result.totalTrades).toBe(0);
    expect(result.totalReturnPct).toBe(0);
  });

  it("captures profitable trades on oscillating series with N=1", () => {
    const candles = makeOscillatingCandles(300);
    const result = runLadderGridBacktest(candles, baseOpts);

    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.winRate).toBeGreaterThan(50);
    expect(result.profitFactor).toBeGreaterThan(1);
    expect(result.totalReturnPct).toBeGreaterThan(0);
  });

  it("multi-rung (N=2, N=3) scales fill capture per oscillation vs N=1", () => {
    // Generate wide oscillations so multiple rungs get touched
    const candles = makeOscillatingCandles(400, 100, 2.0);
    const r1 = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 1,
      gridStepPct: 0.5,
    });
    const r2 = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 2,
      gridStepPct: 0.5,
    });
    const r3 = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 3,
      gridStepPct: 0.5,
    });

    expect(r1.totalTrades).toBeGreaterThan(0);
    expect(r2.totalTrades).toBeGreaterThanOrEqual(r1.totalTrades);
    expect(r3.totalTrades).toBeGreaterThanOrEqual(r2.totalTrades);
  });

  it("progressive arming: rung 2 never fills if price does not reach rung 2", () => {
    // Bar 0: seed open = 100. step = 2.0% = 2.0.
    // Long Rung 1 = 98. Long Rung 2 = 96. Short Rung 1 = 102.
    // Bar 1: low = 97.5 (touches 98 -> fills Rung 1, but doesn't reach 96).
    // Bar 2: high = 100.2 (hits Rung 1 target at 100 -> Rung 1 closes).
    const candles: CandleLike[] = [
      {
        open: 100,
        high: 100.2,
        low: 99.8,
        close: 100,
        volume: 1,
        timestamp: new Date(0),
      },
      {
        open: 100,
        high: 100.2,
        low: 97.5,
        close: 98.5,
        volume: 1,
        timestamp: new Date(15 * 60 * 1000),
      },
      {
        open: 98.5,
        high: 100.2,
        low: 98.0,
        close: 100.0,
        volume: 1,
        timestamp: new Date(30 * 60 * 1000),
      },
    ];

    const result = runLadderGridBacktest(candles, {
      rungs: 2,
      gridStepPct: 2.0,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0,
      slippageBps: 0,
      initialCapital: 100,
      trendFilterPeriod: 0,
      leverage: 1,
    });

    // Only 1 trade executed (Rung 1), Rung 2 never reached
    expect(result.totalTrades).toBe(1);
    expect(result.trades[0].rungIndex).toBe(1);
    expect(result.trades[0].entryPrice).toBe(98);
    expect(result.trades[0].exitPrice).toBe(100);
    expect(result.trades[0].win).toBe(true);
  });

  it("progressive arming: deep wick fills Rung 1 and Rung 2 in order, each with independent TP", () => {
    // step = 2.0 (2%), N = 2. Rung 1 = 98, Rung 2 = 96.
    // Bar 0: seed open = 100.
    // Bar 1: dips to 95.5 -> both Rung 1 (98) and Rung 2 (96) fill!
    // Bar 2: rises to 98.2 -> Rung 2 TP is 96 + 2 = 98 <= 98.2 (Rung 2 closes! Rung 1 TP is 100, stays open)
    // Bar 3: rises to 100.2 -> Rung 1 TP 100 reached (Rung 1 closes!)
    const candles: CandleLike[] = [
      {
        open: 100,
        high: 100.2,
        low: 99.8,
        close: 100,
        volume: 1,
        timestamp: new Date(0),
      },
      {
        open: 100,
        high: 97.0,
        low: 95.5,
        close: 96.5,
        volume: 1,
        timestamp: new Date(15 * 60 * 1000),
      },
      {
        open: 96.5,
        high: 98.2,
        low: 96.0,
        close: 99.0,
        volume: 1,
        timestamp: new Date(30 * 60 * 1000),
      },
      {
        open: 99.0,
        high: 100.2,
        low: 98.8,
        close: 100.0,
        volume: 1,
        timestamp: new Date(45 * 60 * 1000),
      },
    ];

    const result = runLadderGridBacktest(candles, {
      rungs: 2,
      gridStepPct: 2.0,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0,
      slippageBps: 0,
      initialCapital: 100,
      trendFilterPeriod: 0,
      leverage: 1,
    });

    expect(result.totalTrades).toBe(2);
    // Rung 2 closed on Bar 2 (exitBar 2) at 98
    const tradeRung2 = result.trades.find((t) => t.entryPrice === 96);
    expect(tradeRung2).toBeDefined();
    expect(tradeRung2?.exitBar).toBe(2);
    expect(tradeRung2?.exitPrice).toBe(98);
    expect(tradeRung2?.win).toBe(true);

    // Rung 1 closed on Bar 3 (exitBar 3) at 100
    const tradeRung1 = result.trades.find((t) => t.entryPrice === 98);
    expect(tradeRung1).toBeDefined();
    expect(tradeRung1?.exitBar).toBe(3);
    expect(tradeRung1?.exitPrice).toBe(100);
    expect(tradeRung1?.win).toBe(true);
  });

  it("sizes capital per rung as positionFraction / N and compounds capital correctly", () => {
    const candles = makeOscillatingCandles(300);
    const full = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 2,
      positionFraction: 1.0,
    });
    const half = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 2,
      positionFraction: 0.5,
    });

    expect(full.totalTrades).toBe(half.totalTrades);
    expect(half.totalReturnPct).toBeLessThan(full.totalReturnPct);
    expect(half.maxDrawdownPct).toBeLessThanOrEqual(full.maxDrawdownPct);
    expect(half.winRate).toBeCloseTo(full.winRate, 4);
    expect(half.profitFactor).toBeCloseTo(full.profitFactor, 4);
  });

  it("triggers boundary stop-out when price drops beyond base - step * (N + gridMaxGrids)", () => {
    // Base = 100, step = 1.0, N = 2, gridMaxGrids = 2.
    // Boundary = 100 - 1.0 * (2 + 2) = 96.
    // Bar 1 dips to 95.5 <= 96 -> Stop hit!
    const candles: CandleLike[] = [
      {
        open: 100,
        high: 100.2,
        low: 99.8,
        close: 100,
        volume: 1,
        timestamp: new Date(0),
      },
      {
        open: 100,
        high: 100.1,
        low: 95.5,
        close: 95.8,
        volume: 1,
        timestamp: new Date(15 * 60 * 1000),
      },
      {
        open: 95.8,
        high: 96.0,
        low: 95.0,
        close: 95.5,
        volume: 1,
        timestamp: new Date(30 * 60 * 1000),
      },
    ];

    const result = runLadderGridBacktest(candles, {
      rungs: 2,
      gridStepPct: 1.0,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 5,
      feePct: 0,
      slippageBps: 0,
      initialCapital: 100,
      trendFilterPeriod: 0,
      leverage: 1,
    });

    expect(result.totalTrades).toBe(2);
    for (const trade of result.trades) {
      expect(trade.win).toBe(false);
      expect(trade.exitPrice).toBe(96);
      expect(trade.exitReason).toBe("stop");
    }
    expect(result.totalReturnPct).toBeLessThan(0);
  });

  it("pauses entries after a losing stop-out for configured gridPauseAfterLossBars", () => {
    const candles = makeTrendingDownCandles(300);
    const unpaused = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 2,
      gridPauseAfterLossBars: 0,
    });
    const paused = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 2,
      gridPauseAfterLossBars: 10,
    });

    expect(unpaused.totalTrades).toBeGreaterThan(1);
    expect(paused.totalTrades).toBeLessThan(unpaused.totalTrades);
  });

  it("handles leverage liquidation correctly", () => {
    // 20x leverage: Long entry 99 has liq price 99 * (1 - 1/20) = 94.05
    // Stop boundary is 100 - 1 * (1 + 10) = 89 (lower than liquidation)
    // Bar 1 drops to 93.5 <= 94.05 -> Liquidation triggered!
    const candles: CandleLike[] = [
      {
        open: 100,
        high: 100.2,
        low: 99.8,
        close: 100,
        volume: 1,
        timestamp: new Date(0),
      },
      {
        open: 100,
        high: 100.1,
        low: 93.5,
        close: 94.0,
        volume: 1,
        timestamp: new Date(15 * 60 * 1000),
      },
    ];

    const result = runLadderGridBacktest(candles, {
      rungs: 1,
      gridStepPct: 1.0,
      gridMaxGrids: 10,
      gridPauseAfterLossBars: 0,
      feePct: 0,
      slippageBps: 0,
      initialCapital: 100,
      trendFilterPeriod: 0,
      leverage: 20,
    });

    expect(result.totalTrades).toBe(1);
    const trade = result.trades[0];
    expect(trade.isLiquidation).toBe(true);
    expect(trade.win).toBe(false);
    expect(trade.pnlPct).toBe(-1);
    expect(trade.exitPrice).toBeCloseTo(94.05, 4);
    expect(trade.exitReason).toBe("liquidation");
  });

  it("marks open inventory to market at the final candle", () => {
    const candles: CandleLike[] = [];
    for (let i = 0; i < 50; i++) {
      const ts = new Date(i * 15 * 60 * 1000);
      if (i < 49) {
        candles.push({
          open: 100,
          high: 100.1,
          low: 99.9,
          close: 100,
          volume: 1,
          timestamp: ts,
        });
      } else {
        // Last bar drops to fill long rung, does not hit target or stop, then series ends
        candles.push({
          open: 100,
          high: 99.6,
          low: 99.2,
          close: 99.4,
          volume: 1,
          timestamp: ts,
        });
      }
    }

    const result = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 1,
      gridStepPct: 0.5,
    });

    expect(result.totalTrades).toBe(1);
    const lastTrade = result.trades[0];
    expect(lastTrade.exitBar).toBe(49);
    expect(lastTrade.exitPrice).toBe(99.4);
    expect(lastTrade.exitReason).toBe("mark_to_market");
  });

  it("trend filter with SMA and EMA filters entries in direction of trend", () => {
    const candles = makeTrendingDownCandles(150);
    // In a steady downtrend, with onlyWithTrend=true, long entries are suppressed
    const withTrend = runLadderGridBacktest(candles, {
      ...baseOpts,
      trendFilterPeriod: 20,
      trendFilterType: "sma",
      onlyWithTrend: true,
    });

    const withEmaTrend = runLadderGridBacktest(candles, {
      ...baseOpts,
      trendFilterPeriod: 20,
      trendFilterType: "ema",
      onlyWithTrend: true,
    });

    // Both should filter long entries during the downtrend
    expect(withTrend.trades.every((t) => t.side === "short")).toBe(true);
    expect(withEmaTrend.trades.every((t) => t.side === "short")).toBe(true);
  });

  it("chop gate ADX blocks entries during trending periods", () => {
    // Generate trending candles with drift
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
      ...baseOpts,
      rungs: 2,
      trendFilterPeriod: 96,
    };
    const ungated = runLadderGridBacktest(candles, base);
    const gated = runLadderGridBacktest(candles, {
      ...base,
      chopGateAdxThreshold: 25,
    });

    expect(ungated.totalTrades).toBeGreaterThan(0);
    expect(gated.totalTrades).toBe(0);
  });

  it("taker exit fees apply on stop exits", () => {
    const candles = makeTrendingDownCandles(100);
    const symmetric = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 2,
      feePct: 0.04,
    });
    const higherTaker = runLadderGridBacktest(candles, {
      ...baseOpts,
      rungs: 2,
      feePct: 0.04,
      takerExitFeePct: 0.2,
    });

    expect(symmetric.totalTrades).toBeGreaterThan(0);
    expect(higherTaker.totalReturnPct).toBeLessThan(symmetric.totalReturnPct);
  });

  it("deterministic fill simulation with seed", () => {
    const candles = makeRandomWalkCandles(300);
    const cfg: LadderOptions = {
      ...baseOpts,
      rungs: 2,
      makerFillProb: 0.6,
      adverseSelection: true,
      fillSeed: 9999,
    };
    const r1 = runLadderGridBacktest(candles, cfg);
    const r2 = runLadderGridBacktest(candles, cfg);

    expect(r2.totalTrades).toBe(r1.totalTrades);
    expect(r2.totalReturnPct).toBe(r1.totalReturnPct);
    expect(r2.maxDrawdownPct).toBe(r1.maxDrawdownPct);
  });
});

describe("findBestLadderGridParams", () => {
  it("finds the highest returning parameter combo across search space", () => {
    const candles = makeOscillatingCandles(200);
    const best = findBestLadderGridParams(
      candles,
      {
        rungs: [1, 2],
        gridStepPct: [0.3, 0.5],
        gridMaxGrids: [2],
        gridPauseAfterLossBars: [0],
      },
      {
        feePct: 0.04,
        slippageBps: 1,
        initialCapital: 100,
        trendFilterPeriod: 0,
        leverage: 1,
      },
    );

    expect([1, 2]).toContain(best.rungs);
    expect([0.3, 0.5]).toContain(best.gridStepPct);
    expect(best.result.totalTrades).toBeGreaterThan(0);
  });
});

describe("runLadderGridWalkForward", () => {
  it("runs walk-forward windows over candle series", () => {
    const candles = makeOscillatingCandles(600);
    const result = runLadderGridWalkForward(candles, {
      trainWindow: 150,
      testWindow: 100,
      initialCapital: 100,
      searchSpace: {
        rungs: [1, 2],
        gridStepPct: [0.4],
        gridMaxGrids: [2],
        gridPauseAfterLossBars: [0],
      },
      baseOptions: {
        feePct: 0.04,
        slippageBps: 1,
        trendFilterPeriod: 0,
        leverage: 1,
      },
    });

    expect(result.windows.length).toBeGreaterThan(0);
    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.profitableWindowsPct).toBeGreaterThan(0);
  });
});
