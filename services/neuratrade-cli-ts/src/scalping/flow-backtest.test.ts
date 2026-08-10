/**
 * Flow Ignition — flow-v1 backtest harness tests.
 *
 * Synthetic fixtures (no DB, no network):
 *  1. A trending + OI-expanding series must produce LONG signals.
 *  2. A collapsing series must produce SHORT signals.
 *  3. Fees make marginal edges lose (same trades, higher costs).
 *  4. Walk-forward splits respect the purge rule (no label window crosses a
 *     train/test boundary).
 *  5. The report is deterministic (two runs deep-equal).
 */

import { describe, expect, it } from "bun:test";
import {
  computeFlowSignal,
  runFlowBacktest,
  defaultFlowBacktestOptions,
  ATR_STOP_MULT,
  QUARTER_HOUR_MS,
  type FlowBacktestOptions,
  type FlowBacktestReport,
  type FlowSymbolSeries,
  type FlowSignal,
} from "./flow-backtest.js";

// ---------------------------------------------------------------------------
// Fixture builders
// ---------------------------------------------------------------------------

interface FixtureParams {
  /** Number of candles. */
  readonly bars: number;
  /** Candle spacing in ms (60_000 = 1m, 300_000 = 5m). */
  readonly barMs: number;
  readonly startTs: number;
  /** 1 = rising, -1 = collapsing. */
  readonly direction: 1 | -1;
  /** Base per-bar return magnitude (e.g. 0.0004). */
  readonly trendStrength: number;
  /** Return growth across the series (acceleration keeps z-scores hot). */
  readonly acceleration: number;
  /** Base 15m OI fractional change (signed by direction). */
  readonly oiChange: number;
  /** OI-change growth across the series. */
  readonly oiAcceleration: number;
  /** Volume growth factor across the series (1 = flat). */
  readonly volumeRise: number;
  /** Close position within [low, high] at the series start (0..1). */
  readonly buyBiasFrom: number;
  /** Close position within [low, high] at the series end (0..1). */
  readonly buyBiasTo: number;
  /** Constant funding rate per 8h row (0 = no funding rows). */
  readonly fundingRate?: number;
}

function buildSeries(p: FixtureParams): FlowSymbolSeries {
  const candles = [];
  let price = 100;
  const n = p.bars;
  for (let i = 0; i < n; i++) {
    const frac = i / Math.max(1, n - 1);
    const ret = p.direction * (p.trendStrength + p.acceleration * frac);
    const bias = p.buyBiasFrom + (p.buyBiasTo - p.buyBiasFrom) * frac;
    const denom = 2 * bias - 1;
    const open = price;
    // Consistent bar: close = low + (high-low)*bias, so the signed return
    // equals range*(2*bias - 1). denom keeps range > 0 for LONG/SHORT combos.
    const rangePct = denom !== 0 ? ret / denom : 0.0005;
    const low = open * (1 - Math.abs(rangePct));
    const high = open * (1 + Math.abs(rangePct));
    const close = low + (high - low) * bias;
    const volume = 1000 * (1 + p.volumeRise * frac);
    candles.push({
      open,
      high,
      low,
      close,
      volume,
      timestamp: new Date(p.startTs + i * p.barMs),
    });
    price = close;
  }

  // OI points every 15m, fractional change drifting per the params.
  const oi: { ts: number; oi: number }[] = [];
  const oiStep = (15 * 60_000) / p.barMs; // bars per OI point
  let oiValue = 10_000;
  for (let k = 0; k < Math.floor(n / oiStep); k++) {
    const frac = k / Math.max(1, Math.floor(n / oiStep) - 1);
    const dOi = p.direction * (p.oiChange + p.oiAcceleration * frac);
    oi.push({ ts: p.startTs + k * 15 * 60_000, oi: oiValue });
    oiValue = oiValue * (1 + dOi);
  }

  // Funding rows every 8h, constant rate (z_funding = 0, caps pass).
  const funding: { ts: number; fundingRate: number }[] = [];
  if (p.fundingRate !== undefined) {
    const spanMs = n * p.barMs;
    for (let ts = p.startTs; ts < p.startTs + spanMs; ts += 8 * 3_600_000) {
      funding.push({ ts, fundingRate: p.fundingRate });
    }
  }

  return {
    symbol: "TESTUSDT",
    exchange: "bybit",
    timeframe: p.barMs === 60_000 ? "1m" : "5m",
    candles,
    oi,
    funding: funding.length > 0 ? funding : undefined,
  };
}

const TRENDING: FixtureParams = {
  bars: 4 * 24 * 60, // 4 days of 1m bars
  barMs: 60_000,
  startTs: Date.UTC(2026, 5, 1),
  direction: 1,
  trendStrength: 0.0001,
  acceleration: 0.0006,
  oiChange: 0.0005,
  oiAcceleration: 0.002,
  volumeRise: 3,
  buyBiasFrom: 0.55,
  buyBiasTo: 0.92,
  fundingRate: 0.00005,
};

const COLLAPSING: FixtureParams = {
  ...TRENDING,
  direction: -1,
  buyBiasFrom: 0.45,
  buyBiasTo: 0.08,
  fundingRate: -0.00005,
};

function makeOptions(
  overrides: Partial<FlowBacktestOptions> = {},
): FlowBacktestOptions {
  return {
    ...defaultFlowBacktestOptions,
    ...overrides,
    fees: {
      ...defaultFlowBacktestOptions.fees,
      ...(overrides.fees ?? {}),
    },
    thresholds: {
      ...defaultFlowBacktestOptions.thresholds,
      ...(overrides.thresholds ?? {}),
    },
    holdTimes: overrides.holdTimes ?? [...defaultFlowBacktestOptions.holdTimes],
  };
}

/** Single-window backtest options for fast fixtures. */
function fastOptions(overrides: Partial<FlowBacktestOptions> = {}): FlowBacktestOptions {
  return makeOptions({
    trainDays: 2,
    testDays: 1,
    walkForwardSteps: 1,
    ...overrides,
  });
}

function longs(signals: readonly FlowSignal[]): readonly FlowSignal[] {
  return signals.filter((s) => s.side === "LONG");
}

function shorts(signals: readonly FlowSignal[]): readonly FlowSignal[] {
  return signals.filter((s) => s.side === "SHORT");
}

function tradesBySide(
  report: FlowBacktestReport,
  side: "LONG" | "SHORT",
): number {
  return report.portfolio.trades.filter((t) => t.side === side).length;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("flow-v1 signal generation", () => {
  it("produces LONG signals for a trending + OI-expanding series", () => {
    const series = buildSeries(TRENDING);
    const signals = computeFlowSignal(
      series.candles,
      series.oi,
      series.funding,
      makeOptions(),
      series.symbol,
    );
    const long = longs(signals);
    expect(long.length).toBeGreaterThan(0);
    // Signal at a quarter-hour boundary; entry at the bar opening at/after
    // it (no lookahead — the bar at the boundary has not closed yet).
    for (const s of long) {
      expect(s.ts % QUARTER_HOUR_MS).toBe(0);
      expect(s.entryTs).toBeGreaterThanOrEqual(s.ts);
      expect(s.entryTs).toBeLessThan(s.ts + QUARTER_HOUR_MS);
      expect(s.entryPrice).toBeGreaterThan(0);
      expect(s.score).toBeGreaterThanOrEqual(1.0);
      expect(s.zFunding).toBeLessThan(2.0);
    }
  });

  it("produces SHORT signals for a collapsing series", () => {
    const series = buildSeries(COLLAPSING);
    const signals = computeFlowSignal(
      series.candles,
      series.oi,
      series.funding,
      makeOptions(),
      series.symbol,
    );
    const short = shorts(signals);
    expect(short.length).toBeGreaterThan(0);
    for (const s of short) {
      expect(s.score).toBeGreaterThanOrEqual(1.0);
      expect(s.zFunding).toBeGreaterThan(-2.0);
    }
  });

  it("never emits both LONG and SHORT for the same boundary", () => {
    const series = buildSeries(TRENDING);
    const signals = computeFlowSignal(
      series.candles,
      series.oi,
      series.funding,
      makeOptions(),
      series.symbol,
    );
    const keyed = new Map<number, string>();
    for (const s of signals) {
      if (s.side === "NONE") continue;
      const prev = keyed.get(s.ts);
      expect(prev).toBeUndefined();
      keyed.set(s.ts, s.side);
    }
  });
});

describe("flow-v1 walk-forward backtest", () => {
  it("produces LONG trades on the trending fixture and SHORT trades on the collapsing fixture", () => {
    const longSeries = buildSeries(TRENDING);
    const longReport = runFlowBacktest({
      series: [longSeries],
      options: fastOptions(),
    });
    expect(tradesBySide(longReport, "LONG")).toBeGreaterThan(0);
    expect(tradesBySide(longReport, "SHORT")).toBe(0);

    const shortSeries = buildSeries(COLLAPSING);
    const shortReport = runFlowBacktest({
      series: [shortSeries],
      options: fastOptions(),
    });
    expect(tradesBySide(shortReport, "SHORT")).toBeGreaterThan(0);
    expect(tradesBySide(shortReport, "LONG")).toBe(0);

    // Every trade carries costs and a win classification.
    for (const t of longReport.portfolio.trades) {
      expect(t.costPct).toBeGreaterThan(0);
      expect(t.win).toBe(t.netEdgePct > 0);
    }
  });

  it("reports every hold time in the grid", () => {
    const report = runFlowBacktest({
      series: [buildSeries(TRENDING)],
      options: fastOptions({ holdTimes: [0.5, 1, 2, 4, 8] }),
    });
    expect(report.byHoldTime.map((h) => h.holdTimeHours)).toEqual([
      0.5, 1, 2, 4, 8,
    ]);
  });

  it("fees make marginal edges lose", () => {
    const series = buildSeries({
      ...TRENDING,
      // Milder trend: gross edges hover around the default cost (~0.15%).
      trendStrength: 0.00005,
      acceleration: 0.00015,
    });
    const free = runFlowBacktest({
      series: [series],
      options: fastOptions({ fees: { taker: 0, maker: 0 }, spreadBps: 0 }),
    });
    const expensive = runFlowBacktest({
      series: [series],
      options: fastOptions({ fees: { taker: 0.005, maker: 0 }, spreadBps: 2 }),
    });
    const freeTrades = free.portfolio.trades;
    const expTrades = expensive.portfolio.trades;
    expect(freeTrades.length).toBeGreaterThan(0);
    expect(expTrades.length).toBe(freeTrades.length);

    // Same deterministic trade set — only the cost model differs.
    let flippedToLoser = 0;
    for (let i = 0; i < freeTrades.length; i++) {
      const f = freeTrades[i];
      const e = expTrades[i];
      expect(e.entryTs).toBe(f.entryTs);
      expect(e.exitTs).toBe(f.exitTs);
      expect(e.netEdgePct).toBeLessThan(f.netEdgePct);
      if (f.netEdgePct > 0 && e.netEdgePct <= 0) flippedToLoser++;
    }
    expect(flippedToLoser).toBeGreaterThan(0);
    expect(expensive.portfolio.winRate).toBeLessThan(free.portfolio.winRate);
    expect(expensive.portfolio.avgEdgePerTradePct).toBeLessThan(
      free.portfolio.avgEdgePerTradePct,
    );
  });

  it("walk-forward splits respect the purge rule", () => {
    // 12 days of 5m bars; 3 windows of train 3d / test 2d.
    const series = buildSeries({
      bars: 12 * 24 * 12,
      barMs: 300_000,
      startTs: Date.UTC(2026, 2, 1),
      direction: 1,
      trendStrength: 0.0001,
      acceleration: 0.0004,
      oiChange: 0.0005,
      oiAcceleration: 0.001,
      volumeRise: 2,
      buyBiasFrom: 0.55,
      buyBiasTo: 0.9,
      fundingRate: 0.00005,
    });
    const report = runFlowBacktest({
      series: [series],
      options: makeOptions({
        trainDays: 3,
        testDays: 2,
        walkForwardSteps: 3,
        holdTimes: [0.5, 1, 2, 4, 8],
      }),
    });
    expect(report.windows.length).toBe(3);
    // Every window must carry signals — a window with 0 would hide a leak
    // (signals mis-routed past the first test segment).
    for (const w of report.windows) {
      expect(w.signals).toBeGreaterThan(0);
    }

    const maxHoldMs = Math.max(...report.options.holdTimes) * 3_600_000;
    for (const window of report.windows) {
      for (const hold of report.byHoldTime) {
        for (const t of hold.trades) {
          // The trade's entry must sit inside THIS test segment, and its
          // full label window (entry through entry + max hold) must not
          // cross the segment's end — the purge rule.
          if (t.entryTs >= window.testStart && t.entryTs < window.testEnd) {
            expect(t.entryTs + maxHoldMs).toBeLessThanOrEqual(window.testEnd);
            expect(t.exitTs).toBeLessThanOrEqual(window.testEnd);
          }
        }
      }
    }
    // Every trade belongs to exactly one window.
    const allTrades = report.portfolio.trades;
    for (const t of allTrades) {
      const owners = report.windows.filter(
        (w) => t.entryTs >= w.testStart && t.entryTs < w.testEnd,
      );
      expect(owners.length).toBe(1);
    }
  });

  it("purges signals whose label window would cross the boundary", () => {
    // Short series: the last test window cannot fit a full 8h label at its
    // end, so the harness must drop those signals rather than leak them.
    const series = buildSeries({
      bars: 8 * 24 * 12,
      barMs: 300_000,
      startTs: Date.UTC(2026, 2, 1),
      direction: 1,
      trendStrength: 0.0001,
      acceleration: 0.0004,
      oiChange: 0.0005,
      oiAcceleration: 0.001,
      volumeRise: 2,
      buyBiasFrom: 0.55,
      buyBiasTo: 0.9,
      fundingRate: 0.00005,
    });
    const report = runFlowBacktest({
      series: [series],
      options: makeOptions({
        trainDays: 2,
        testDays: 2,
        walkForwardSteps: 3,
        holdTimes: [8],
      }),
    });
    // windows = min(3, floor((8-2)/2)) = 3; the 3rd test window ends at t0+8d.
    const last = report.windows[report.windows.length - 1];
    expect(last).toBeDefined();
    for (const t of report.portfolio.trades) {
      expect(t.entryTs + 8 * 3_600_000).toBeLessThanOrEqual(last.testEnd);
    }
  });

  it("is deterministic: two runs produce identical reports", () => {
    const series = buildSeries(TRENDING);
    const options = fastOptions();
    const data = { series: [series], options };
    const a = runFlowBacktest(data);
    const b = runFlowBacktest(data);
    expect(JSON.stringify(a)).toBe(JSON.stringify(b));
  });

  it("reports zero trades when history is too short for walk-forward", () => {
    const series = buildSeries({
      ...TRENDING,
      bars: 24 * 60, // 1 day — shorter than train + test
    });
    const report = runFlowBacktest({
      series: [series],
      options: makeOptions({ trainDays: 2, testDays: 1, walkForwardSteps: 1 }),
    });
    expect(report.windows.length).toBe(0);
    expect(report.portfolio.totalTrades).toBe(0);
  });
});
