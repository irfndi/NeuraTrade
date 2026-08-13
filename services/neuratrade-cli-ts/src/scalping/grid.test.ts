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

/** Random walk with real intrabar wicks — produces genuine close-through vs
 *  recovered-wick touches so the adverse-selection fill path is exercised. */
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

/** Steady downtrend: long grid entries repeatedly hit their stops, guaranteeing
 *  taker stop-exits to exercise the taker-exit fee path. */
function makeTrendingDownCandles(count: number): CandleLike[] {
  const candles: CandleLike[] = [];
  let price = 100;
  for (let i = 0; i < count; i++) {
    const open = price;
    const close = price * 0.995;
    candles.push({
      open,
      high: open * 1.001,
      low: close * 0.999,
      close,
      volume: 1,
      timestamp: new Date(i * 15 * 60 * 1000),
    });
    price = close;
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
      expect(t.pnlPct).toBeTypeOf("number");
      expect(t.win).toBe(t.pnlPct > 0);
      expect(["long", "short"]).toContain(t.side);
      expect(t.isLiquidation).toBeTypeOf("boolean");
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

  it("makerFillProb=1 reproduces the default (touched = filled) behavior", () => {
    const candles = makeOscillatingCandles(300);
    const baseline = runGridBacktest(candles, sizingOpts);
    const explicit = runGridBacktest(candles, {
      ...sizingOpts,
      makerFillProb: 1,
    });
    expect(explicit.totalTrades).toBe(baseline.totalTrades);
    expect(explicit.totalReturnPct).toBeCloseTo(baseline.totalReturnPct, 8);
    expect(explicit.maxDrawdownPct).toBeCloseTo(baseline.maxDrawdownPct, 8);
  });

  it("adverse selection is inert at makerFillProb=1 (every touch fills)", () => {
    const candles = makeRandomWalkCandles(400);
    const full = runGridBacktest(candles, sizingOpts);
    const adverseFull = runGridBacktest(candles, {
      ...sizingOpts,
      makerFillProb: 1,
      adverseSelection: true,
    });
    expect(adverseFull.totalTrades).toBe(full.totalTrades);
    expect(adverseFull.totalReturnPct).toBeCloseTo(full.totalReturnPct, 8);
  });

  it("fill decisions are deterministic for a fixed seed", () => {
    const candles = makeRandomWalkCandles(400);
    const cfg = {
      ...sizingOpts,
      makerFillProb: 0.6,
      adverseSelection: true,
      takerExitFeePct: 0.06,
    };
    const first = runGridBacktest(candles, cfg);
    const second = runGridBacktest(candles, cfg);
    expect(second.totalTrades).toBe(first.totalTrades);
    expect(second.totalReturnPct).toBe(first.totalReturnPct);
  });

  it("adverse selection shifts realized fills toward losers (lower win rate)", () => {
    const candles = makeRandomWalkCandles(800);
    const uniform = runGridBacktest(candles, {
      ...sizingOpts,
      makerFillProb: 0.6,
      adverseSelection: false,
    });
    const adverse = runGridBacktest(candles, {
      ...sizingOpts,
      makerFillProb: 0.6,
      adverseSelection: true,
    });
    expect(adverse.totalTrades).toBeGreaterThan(0);
    expect(adverse.winRate).toBeLessThanOrEqual(uniform.winRate);
    expect(adverse.totalReturnPct).toBeLessThanOrEqual(uniform.totalReturnPct);
  });

  it("sub-unity makerFillProb fills strictly fewer entries", () => {
    const candles = makeOscillatingCandles(400);
    const full = runGridBacktest(candles, sizingOpts);
    const partial = runGridBacktest(candles, {
      ...sizingOpts,
      makerFillProb: 0.3,
    });
    expect(full.totalTrades).toBeGreaterThan(0);
    expect(partial.totalTrades).toBeLessThan(full.totalTrades);
  });

  it("taker stop fees reduce returns relative to symmetric fees", () => {
    const candles = makeTrendingDownCandles(200);
    const cfg = {
      gridStepPct: 0.5,
      gridMaxGrids: 1.5,
      gridPauseAfterLossBars: 0,
      feePct: 0.02,
      slippageBps: 0,
      initialCapital: 10000,
      trendFilterPeriod: 1,
      leverage: 1,
    };
    const symmetric = runGridBacktest(candles, cfg);
    const taker = runGridBacktest(candles, { ...cfg, takerExitFeePct: 0.3 });
    expect(symmetric.totalTrades).toBeGreaterThan(0);
    expect(taker.totalReturnPct).toBeLessThan(symmetric.totalReturnPct);
  });

  it("winning target exits do not suppress subsequent entries (pause is loss-only)", () => {
    // makeOscillatingCandles alternates strictly up/down bars, so every round
    // trip exits at the target (a win); the series ends with one open long
    // closed by the mark-to-market block. With a pause configured, the OLD
    // behavior paused after wins too, skipping bars and producing fewer
    // trades; the loss-only contract must yield an identical trade list.
    const candles = makeOscillatingCandles(600);
    const base = {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 0,
      leverage: 1,
    };
    const unpaused = runGridBacktest(candles, {
      ...base,
      gridPauseAfterLossBars: 0,
    });
    const pauseConfigured = runGridBacktest(candles, {
      ...base,
      gridPauseAfterLossBars: 8,
    });
    expect(unpaused.totalTrades).toBeGreaterThan(1);
    expect(pauseConfigured.totalTrades).toBe(unpaused.totalTrades);
  });

  it("losing stop exits still pause subsequent entries", () => {
    // Steady downtrend: long entries repeatedly hit their stops (losses), so
    // the loss-pause must throttle the trade count relative to no pause.
    const candles = makeTrendingDownCandles(400);
    const base = {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 0,
      leverage: 1,
    };
    const unpaused = runGridBacktest(candles, {
      ...base,
      gridPauseAfterLossBars: 0,
    });
    const paused = runGridBacktest(candles, {
      ...base,
      gridPauseAfterLossBars: 5,
    });
    expect(unpaused.totalTrades).toBeGreaterThan(1);
    expect(paused.totalTrades).toBeLessThan(unpaused.totalTrades);
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

  it("marks open inventory to market at the final candle (no silent boundary erase)", () => {
    const candles: CandleLike[] = [];
    for (let i = 0; i < 100; i++) {
      const ts = new Date(i * 15 * 60 * 1000);
      if (i < 99) {
        candles.push({
          open: 100,
          high: 100.1,
          low: 99.9,
          close: 100,
          volume: 1,
          timestamp: ts,
        });
      } else {
        // Last candle dips below the buy level, opening a long that the
        // series ends before it can hit target/stop/liquidation.
        candles.push({
          open: 100,
          high: 100.1,
          low: 99.0,
          close: 99.2,
          volume: 1,
          timestamp: ts,
        });
      }
    }

    const result = runGridBacktest(candles, {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 0,
      leverage: 1,
    });

    // The open position must be closed at the last close, not silently
    // dropped (which would report 0 trades and a flat 0% return).
    expect(result.totalTrades).toBe(1);
    const last = result.trades[result.trades.length - 1];
    expect(last.side).toBe("long");
    expect(last.exitBar).toBe(99);
    expect(last.exitPrice).toBe(99.2);
    expect(result.totalReturnPct).toBeLessThan(0);
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

  it("reports trade-weighted avg win/loss on a mixed series (both sides present)", () => {
    // Seeded random walk with real intrabar wicks — genuine wins AND losses.
    const candles = makeRandomWalkCandles(900, 7);
    const result = runGridWalkForward(candles, {
      trainWindow: 200,
      testWindow: 150,
      initialCapital: 20,
      searchSpace: {
        gridStepPct: [0.3],
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

    expect(result.totalTrades).toBeGreaterThan(0);
    // Both sides trade on the wick-rich series: averages must exist and be
    // positive — these feed the funnel's structural-asymmetry gate.
    expect(result.avgWinPct).toBeGreaterThan(0);
    expect(result.avgLossPct).toBeGreaterThan(0);
    for (const window of result.windows) {
      if (window.testTrades === 0) continue;
      if (window.avgWinPct !== undefined) {
        expect(window.avgWinPct).toBeGreaterThan(0);
      }
      if (window.avgLossPct !== undefined) {
        expect(window.avgLossPct).toBeGreaterThan(0);
      }
    }
  });

  it("reports only the loss side on a monotonic downtrend (extreme asymmetry)", () => {
    // -0.5%/bar downtrend: the grid only ever takes long entries (no bar
    // reaches the sell level) and every long hits its stop — the walk-
    // forward must expose this as avgLoss only, so the asymmetry gate can
    // reject the config instead of trusting aggregate return.
    const candles = makeTrendingDownCandles(900);
    const result = runGridWalkForward(candles, {
      trainWindow: 200,
      testWindow: 150,
      initialCapital: 20,
      searchSpace: {
        gridStepPct: [0.3],
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

    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.avgWinPct).toBeUndefined();
    expect(result.avgLossPct ?? 0).toBeGreaterThan(0);
  });
});
