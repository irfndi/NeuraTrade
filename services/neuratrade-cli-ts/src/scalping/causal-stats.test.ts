import { describe, expect, it } from "bun:test";
import {
  computeSymbolStats,
  makeCausalSymbolStats,
  type SymbolStatistics,
} from "./symbol-stats.js";
import type { CandleLike } from "./types.js";

function candle(
  close: number,
  index: number,
  spread = 0.01,
  volume = 100,
): CandleLike {
  const open = close * (1 - spread / 2);
  const high = close * (1 + spread / 2);
  const low = close * (1 - spread / 1.5);
  return {
    open,
    high,
    low,
    close,
    volume,
    timestamp: new Date(1_700_000_000_000 + index * 300_000),
  };
}

/** Random-walk-ish series with a volatility regime shift at `shiftAt`. */
function makeSeries(length: number, shiftAt: number, seed = 42): CandleLike[] {
  let state = seed;
  const rand = () => {
    state = (state * 1103515245 + 12345) % 2147483648;
    return state / 2147483648 - 0.5;
  };
  const candles: CandleLike[] = [];
  let price = 100;
  for (let i = 0; i < length; i++) {
    const vol = i >= shiftAt ? 0.035 : 0.008;
    price = Math.max(1, price * (1 + rand() * vol));
    candles.push(candle(price, i, vol / 2, 100 + Math.abs(rand()) * 50));
  }
  return candles;
}

describe("makeCausalSymbolStats", () => {
  it("never uses future data: stats at bar i are identical when the future changes", () => {
    const base = makeSeries(400, 250);
    const alteredFuture = [...base];
    for (let i = 251; i < alteredFuture.length; i++) {
      const c = alteredFuture[i];
      alteredFuture[i] = {
        ...c,
        high: c.high * 3,
        low: c.low * 0.5,
        close: c.close * 2,
        volume: c.volume * 10,
      };
    }

    const providerA = makeCausalSymbolStats(base, "5m");
    const providerB = makeCausalSymbolStats(alteredFuture, "5m");

    for (const i of [20, 50, 100, 200, 249, 250]) {
      expect(providerB(i)).toEqual(providerA(i));
    }
  });

  it("converges to batch computeSymbolStats on the same prefix for long stable series", () => {
    const series = makeSeries(500, 10_000); // no regime shift
    const i = 499;
    const causal = makeCausalSymbolStats(series, "5m")(i);
    const batch = computeSymbolStats(series.slice(0, i + 1), "5m");
    expect(causal.atr14Pct).toBeCloseTo(batch.atr14Pct, 6);
    expect(causal.atrPctMedian).toBeCloseTo(batch.atrPctMedian, 2);
    expect(causal.atrPct20).toBeCloseTo(batch.atrPct20, 2);
    expect(causal.atrPct80).toBeCloseTo(batch.atrPct80, 2);
    expect(causal.adx14).toBeCloseTo(batch.adx14, 4);
    expect(causal.volumeRatio).toBeCloseTo(batch.volumeRatio, 6);
  });

  it("matches the batch ADX14 exactly at every bar from the first valid index (seed-window parity)", () => {
    // Regression lock for the reported "causal ADX seed window off-by-one":
    // the causal Wilder ADX must equal calculateADX over each prefix at every
    // bar, including the very first valid value (i = 2*period). A seed-window
    // shift would surface here as a divergence from the first valid bar on.
    const series = makeSeries(400, 10_000);
    const provider = makeCausalSymbolStats(series, "5m");
    const period = 14;
    for (let i = period * 2; i < series.length; i++) {
      const causal = provider(i).adx14;
      const batch = computeSymbolStats(series.slice(0, i + 1), "5m").adx14;
      expect(causal).toBeCloseTo(batch, 6);
    }
  });

  it("returns zero-ish stats for very short series", () => {
    const provider = makeCausalSymbolStats(makeSeries(5, 10_000), "5m");
    const stats = provider(4);
    expect(stats.atr14Pct).toBe(0);
    expect(stats.adx14).toBe(0);
    expect(stats.isTrending).toBe(false);
  });

  it("reflects regime shifts quickly (no stale end-of-series values)", () => {
    const series = makeSeries(400, 250);
    const provider = makeCausalSymbolStats(series, "5m");
    const before = provider(200);
    const after = provider(350);
    // The series doubles its volatility at bar 250: causal ATR% must rise.
    expect(after.atr14Pct).toBeGreaterThan(before.atr14Pct * 1.5);
  });
});

describe("runBacktest causal-stats reconciliation (bd clever-cabin-dt8)", () => {
  it("FULL result is consistent with IS+OOS split results", async () => {
    const { runBacktest } = await import("./backtest.js");
    const { defaultComposerConfig } = await import("./composer.js");
    const series = makeSeries(2000, 1200);
    const options = {
      symbol: "TEST/USDT",
      exchange: "test",
      timeframe: "5m",
      candles: series,
      composerConfig: defaultComposerConfig,
      initialCapital: 10_000,
      positionSizePct: 100,
      stopLossPct: 0,
      takeProfitPct: 0,
      feePct: 0.06,
      minConfidence: 0.35,
      useAtrStops: true,
      atrStopMultiplier: 1,
      atrTakeProfitMultiplier: 2,
      isFutures: true,
      slippageBps: 2,
      leverage: 1,
      maxBarsInTrade: 12,
      htfCandles: [],
      recordEquityCurve: false,
    } as const;

    const full = runBacktest({ ...options });
    const split = runBacktest({ ...options, oosPct: 20 });
    const isRes = split;
    const oosRes = split.oosResult!;

    // Trade counts must be close (boundary effects only).
    const combined = isRes.totalTrades + oosRes.totalTrades;
    expect(Math.abs(combined - full.totalTrades)).toBeLessThanOrEqual(
      Math.max(3, Math.round(full.totalTrades * 0.1)),
    );
    // Compounded IS+OOS return must be near the FULL return (no look-ahead inflation).
    const compounded =
      (1 + isRes.totalReturnPct / 100) * (1 + oosRes.totalReturnPct / 100) - 1;
    expect(Math.abs(compounded * 100 - full.totalReturnPct)).toBeLessThan(5);
  }, 15_000);
});
